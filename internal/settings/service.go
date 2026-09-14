package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/httpx"
	"github.com/FeiBaiKin/lumo/internal/secret"
	"github.com/FeiBaiKin/lumo/internal/slug"
)

// 错误哨兵。
var (
	// ErrUnknownGroup 表示分组未注册。
	ErrUnknownGroup = errors.New("设置分组不存在")
)

// ValidationError 表示值未通过 Schema 或 Go 侧校验，Details 逐条定位到字段。
type ValidationError struct {
	Details []httpx.ErrorDetail
}

// Error 实现 error。
func (e *ValidationError) Error() string {
	if len(e.Details) == 0 {
		return "设置校验失败"
	}
	parts := make([]string, 0, len(e.Details))
	for _, d := range e.Details {
		if d.Location != "" {
			parts = append(parts, d.Location+": "+d.Message)
		} else {
			parts = append(parts, d.Message)
		}
	}
	return "设置校验失败：" + strings.Join(parts, "；")
}

// cacheTTL 是有效值的进程内缓存时长：多实例部署时其他实例的写入最迟这么久后可见。
const cacheTTL = 30 * time.Second

// groupNamePattern 限定分组名形态（DNS-1123）。
var groupNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Group 是已编译的设置分组。
type Group struct {
	app.SettingGroup
	schema   *jsonschema.Schema
	doc      map[string]any
	defaults map[string]any
	// secrets 是本分组里以 form.Secret 声明的顶层字段名：进库前加密，出接口前抹掉。
	secrets []string
}

// SchemaDoc 返回解析后的 Schema 文档，供接口输出。
func (g *Group) SchemaDoc() map[string]any { return g.doc }

// Defaults 返回缺省值的副本。
func (g *Group) DefaultValues() map[string]any { return maps.Clone(g.defaults) }

type cacheEntry struct {
	values  map[string]any
	expires time.Time
}

// ValueStore 是设置值的持久化接口。
//
// 抽成接口不是为了将来换数据库——它的实现只有 Store 一个——而是为了让
// 「口令进库是密文」这类断言能在纯内存的测试里验：真去连一个 PG 才能验加密，
// 代价是这条最要紧的性质在最常见的开发环境里跑不起来。
type ValueStore interface {
	// Load 读取分组的已保存值；从未保存过时返回空对象。
	Load(ctx context.Context, name string) (map[string]any, error)
	// Save 覆盖式写入分组的值。
	Save(ctx context.Context, name string, values map[string]any) error
}

// Service 是设置的读写入口：持有已注册分组、校验器与缓存。
type Service struct {
	store ValueStore

	// keyring 与 logger 由模块在 Start 时注入：主密钥的加载会碰文件系统，
	// 而没有任何分组声明口令字段的站点不该在磁盘上多出一个必须备份的密钥文件。
	keyring *secret.Keyring
	logger  *slog.Logger

	mu     sync.RWMutex
	groups map[string]*Group
	order  []string
	cache  map[string]cacheEntry
}

// NewService 构造 Service；store 可为 nil（仅做 Schema 校验的场景）。
func NewService(store ValueStore) *Service {
	return &Service{store: store, groups: map[string]*Group{}, cache: map[string]cacheEntry{}}
}

// SetKeyring 注入主密钥，供口令字段加解密。须在开始服务之前调用。
func (s *Service) SetKeyring(k *secret.Keyring) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyring = k
}

// SetLogger 注入日志器；口令解不开时靠它留一条线索。
func (s *Service) SetLogger(l *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logger = l
}

// HasSecrets 报告已注册分组里有没有口令字段。
//
// 模块据此决定要不要去加载主密钥：没有口令字段就不碰密钥文件，
// 免得每个只装了本站的目录里都躺着一个没人知道该不该备份的 secret.key。
func (s *Service) HasSecrets() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, name := range s.order {
		if len(s.groups[name].secrets) > 0 {
			return true
		}
	}
	return false
}

// RegisterGroups 编译并登记分组。任一分组的 Schema 或 Defaults 不合法即整体失败：
// 那是模块作者的错误，必须在启动时暴露。
func (s *Service) RegisterGroups(groups []app.SettingGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range groups {
		g, err := compileGroup(&groups[i])
		if err != nil {
			return err
		}
		if _, dup := s.groups[g.Name]; dup {
			return fmt.Errorf("settings: 分组 %q 重复注册", g.Name)
		}
		// 口令字段一旦出现在 Public 白名单里，前台就能读到它——
		// 这是配置事故里最容易发生也最不该发生的一种，在注册这一刻直接拦下。
		for _, key := range g.secrets {
			if slices.Contains(g.Public, key) {
				return fmt.Errorf("settings: 分组 %q 把口令字段 %q 列进了 Public，前台会读到它", g.Name, key)
			}
		}
		s.groups[g.Name] = g
		s.order = append(s.order, g.Name)
	}
	sort.SliceStable(s.order, func(i, j int) bool {
		a, b := s.groups[s.order[i]], s.groups[s.order[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Name < b.Name
	})
	return nil
}

// compileGroup 校验声明并编译 Schema。
func compileGroup(decl *app.SettingGroup) (*Group, error) {
	if !groupNamePattern.MatchString(decl.Name) {
		return nil, fmt.Errorf("settings: 非法的分组名 %q", decl.Name)
	}
	if decl.Form == nil {
		return nil, fmt.Errorf("settings: 分组 %q 缺少表单声明", decl.Name)
	}
	validator, err := NewValidator(decl.Name, decl.Form)
	if err != nil {
		return nil, err
	}

	defaults := validator.DefaultValues()
	g := &Group{
		SettingGroup: *decl,
		schema:       validator.schema,
		doc:          validator.Doc(),
		defaults:     defaults,
		secrets:      decl.Form.SecretKeys(),
	}
	// 缺省值必须自洽：模块作者把缺省值写成不合自身约束时，
	// 站长打开设置页会看到一堆无法保存的初始值。
	if err := g.validate(defaults); err != nil {
		return nil, fmt.Errorf("settings: 分组 %q 的 Defaults 未通过自身 Schema: %w", decl.Name, err)
	}
	return g, nil
}

// validate 用 Schema 与 Check 校验有效值。
func (g *Group) validate(values map[string]any) error {
	if err := g.schema.Validate(normalize(values)); err != nil {
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			return &ValidationError{Details: collectDetails(verr.BasicOutput(), nil)}
		}
		return &ValidationError{Details: []httpx.ErrorDetail{{Message: err.Error()}}}
	}
	// 条件必填：Schema 只管无条件的必填，带 x-show-if 的字段由表单声明判定。
	if missing := g.Form.Missing(values); len(missing) > 0 {
		details := make([]httpx.ErrorDetail, 0, len(missing))
		for _, path := range missing {
			details = append(details, httpx.ErrorDetail{Location: "body." + path, Message: "不能为空"})
		}
		return &ValidationError{Details: details}
	}
	if g.Check != nil {
		if err := g.Check(values); err != nil {
			var verr *ValidationError
			if errors.As(err, &verr) {
				return verr
			}
			return &ValidationError{Details: []httpx.ErrorDetail{{Message: err.Error(), Location: "body"}}}
		}
	}
	return nil
}

// normalize 把值经 JSON 往返一次，让整数等类型与校验器的期望一致。
func normalize(values map[string]any) any {
	raw, err := json.Marshal(values)
	if err != nil {
		return values
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return values
	}
	return doc
}

// collectDetails 把校验器的树状输出摊平成逐条明细。
func collectDetails(unit *jsonschema.OutputUnit, out []httpx.ErrorDetail) []httpx.ErrorDetail {
	if unit == nil {
		return out
	}
	if unit.Error != nil && len(unit.Errors) == 0 {
		out = append(out, httpx.ErrorDetail{
			Message:  unit.Error.String(),
			Location: "body" + strings.ReplaceAll(unit.InstanceLocation, "/", "."),
		})
	}
	for i := range unit.Errors {
		out = collectDetails(&unit.Errors[i], out)
	}
	return out
}

// Validator 是一份编译好的表单校验器，供设置分组之外的场景复用。
//
// 主题设置的声明由主题包的 settings.yaml 给出、随主题安装而变，
// 不能走 RegisterGroups 那条「启动期固定登记」的路径；但校验语义必须与站点设置完全一致，
// 否则主题作者要面对两套规则。故把编译与校验单独暴露出来（agent.md §5）。
//
// 校验分两步，缺一不可：JSON Schema 判「值是否合法」，Form.Missing 判
// 「此刻该显示的必填项是否都填了」。后者只能由 Form 回答——条件依赖的可见性是
// 它的知识，Schema 里只留了一句 x-show-if 的提示。
type Validator struct {
	form   *form.Form
	schema *jsonschema.Schema
}

// NewValidator 编译一份表单声明。
//
// name 只用于错误信息与内部资源定位，不要求全局唯一。
func NewValidator(name string, f *form.Form) (*Validator, error) {
	if f == nil {
		return nil, fmt.Errorf("settings: %s 缺少表单声明", name)
	}
	doc, _, err := f.Build()
	if err != nil {
		return nil, fmt.Errorf("settings: %s 的表单声明有误: %w", name, err)
	}
	if typ, _ := doc["type"].(string); typ != "object" {
		return nil, fmt.Errorf("settings: %s 的 Schema 顶层 type 须为 object", name)
	}

	compiler := jsonschema.NewCompiler()
	location := "lumo://schema/" + name + ".json"
	if addErr := compiler.AddResource(location, doc); addErr != nil {
		return nil, fmt.Errorf("settings: 载入 %s 的 Schema: %w", name, addErr)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("settings: 编译 %s 的 Schema: %w", name, err)
	}
	return &Validator{form: f, schema: compiled}, nil
}

// Doc 返回解析后的 Schema 文档，供接口原样输出给表单引擎。
func (v *Validator) Doc() map[string]any { return v.form.Doc() }

// DefaultValues 返回缺省值的副本。
func (v *Validator) DefaultValues() map[string]any { return v.form.DefaultValues() }

// Missing 返回此刻应当显示却没填的必填字段，供调用方在保存前判定。
func (v *Validator) Missing(values map[string]any) []string { return v.form.Missing(values) }

// Validate 校验一个值对象，失败时返回 *ValidationError（明细逐条定位到字段）。
func (v *Validator) Validate(values map[string]any) error {
	if err := v.schema.Validate(normalize(values)); err != nil {
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			return &ValidationError{Details: collectDetails(verr.BasicOutput(), nil)}
		}
		return &ValidationError{Details: []httpx.ErrorDetail{{Message: err.Error()}}}
	}
	return v.validateVisibleRequired(values)
}

// validateVisibleRequired 追究「条件成立但没填」的必填字段。
//
// 这一条靠 JSON Schema 做不了：带 x-show-if 的字段在条件不成立时压根不存在，
// 把它写进 required 会让「隐藏起来所以没填」变成一次校验失败。
// 可见性是表单声明的知识，只能回到 Form 上问。
func (v *Validator) validateVisibleRequired(values map[string]any) error {
	missing := v.form.Missing(values)
	if len(missing) == 0 {
		return nil
	}
	details := make([]httpx.ErrorDetail, 0, len(missing))
	for _, path := range missing {
		details = append(details, httpx.ErrorDetail{
			Location: "body." + path,
			Message:  "不能为空",
		})
	}
	return &ValidationError{Details: details}
}

// Merge 返回 defaults 被 overrides 按顶层键覆盖后的新对象，与设置分组的合并语义一致。
func Merge(defaults, overrides map[string]any) map[string]any {
	return merge(defaults, overrides)
}

// Groups 返回全部分组，按 Order、Name 排序。
func (s *Service) Groups() []*Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Group, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.groups[name])
	}
	return out
}

// Group 按名称取分组。
func (s *Service) Group(name string) (*Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groups[name]
	return g, ok
}

// Effective 返回分组的有效值（缺省值被已保存值按顶层键覆盖）的副本。
func (s *Service) Effective(ctx context.Context, name string) (map[string]any, error) {
	g, ok := s.Group(name)
	if !ok {
		return nil, ErrUnknownGroup
	}

	s.mu.RLock()
	entry, cached := s.cache[name]
	s.mu.RUnlock()
	if cached && time.Now().Before(entry.expires) {
		return maps.Clone(entry.values), nil
	}

	stored := map[string]any{}
	if s.store != nil {
		loaded, err := s.store.Load(ctx, name)
		if err != nil {
			return nil, err
		}
		stored = loaded
	}
	values := merge(g.defaults, s.openSecrets(g, stored))

	s.mu.Lock()
	s.cache[name] = cacheEntry{values: values, expires: time.Now().Add(cacheTTL)}
	s.mu.Unlock()
	return maps.Clone(values), nil
}

// Get 把分组的有效值解码到 out（通常是带 json 标签的结构体）。
func (s *Service) Get(ctx context.Context, name string, out any) error {
	values, err := s.Effective(ctx, name)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("编码设置 %s: %w", name, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解码设置 %s: %w", name, err)
	}
	return nil
}

// Update 用给定值覆盖分组：先与缺省值合并成完整对象，校验通过后整体保存。
//
// **不返回有效值**：有效值里含解密后的口令，返回它等于把明文口令递给每一个调用方，
// 而接口要的是抹掉口令的视图（见 Group.Mask）。少一个返回值就少一条泄漏路径。
func (s *Service) Update(ctx context.Context, name string, values map[string]any) error {
	g, ok := s.Group(name)
	if !ok {
		return ErrUnknownGroup
	}
	resolved, err := s.resolveSecrets(ctx, g, values)
	if err != nil {
		return err
	}
	// 校验的是明文：Schema 与 Check 声明的是「口令长什么样」，
	// 拿密文去校验的话，口令一存进去就再也过不了自己的规则。
	effective := merge(g.defaults, resolved)
	// 写成独立语句而不是 if 的初始化子句：那里新声明 err 会遮住上面这个，
	// 而下面还要用它接 sealSecrets 的结果；不新声明又会招来 sloppyReassign。
	err = g.validate(effective)
	if err != nil {
		return err
	}
	if s.store == nil {
		return errors.New("settings: 未配置存储，无法写入")
	}
	sealed, err := s.sealSecrets(g, effective)
	if err != nil {
		return err
	}
	if err := s.store.Save(ctx, name, sealed); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.cache, name)
	s.mu.Unlock()
	return nil
}

// resolveSecrets 定下本次提交里每个口令字段最终要存的值。
//
// 三态是刻意的，为的是让「不改动」成为缺省行为：
// 缺席或空串 = 保持原值（表单上那个框本来就是空的，把空当成「清空」会让
// 每次改别的字段都顺手抹掉口令），JSON null = 清除，非空串 = 设为该值。
func (s *Service) resolveSecrets(ctx context.Context, g *Group, values map[string]any) (map[string]any, error) {
	if len(g.secrets) == 0 {
		return values, nil
	}
	current, err := s.Effective(ctx, g.Name)
	if err != nil {
		return nil, err
	}
	out := maps.Clone(values)
	for _, key := range g.secrets {
		raw, present := out[key]
		switch {
		case present && raw == nil:
			out[key] = ""
		case !present || raw == "":
			out[key] = current[key]
		default:
			if _, ok := raw.(string); !ok {
				return nil, &ValidationError{Details: []httpx.ErrorDetail{{
					Location: "body." + key, Message: "口令必须是文本"}}}
			}
		}
	}
	return out, nil
}

// sealSecrets 在写库前把口令字段换成密文。
func (s *Service) sealSecrets(g *Group, values map[string]any) (map[string]any, error) {
	if len(g.secrets) == 0 {
		return values, nil
	}
	keyring := s.keyringNow()
	if keyring == nil {
		return nil, errors.New("settings: 分组声明了口令字段，但没有加载主密钥，拒绝以明文写入")
	}
	out := maps.Clone(values)
	for _, key := range g.secrets {
		plain, ok := out[key].(string)
		if !ok || plain == "" {
			continue
		}
		sealed, err := keyring.Seal(plain)
		if err != nil {
			return nil, fmt.Errorf("加密设置 %s 的 %s: %w", g.Name, key, err)
		}
		out[key] = sealed
	}
	return out, nil
}

// openSecrets 在读库后把口令字段换回明文。
//
// 解不开时按「未设置」处理并记一条 warn，**不让它变成错误**：
// 密钥文件被换掉之后，站长打开设置页要能填回口令——这里返回错误的话，
// 整页会变成 500，他连重填的入口都没有了。
func (s *Service) openSecrets(g *Group, values map[string]any) map[string]any {
	if len(g.secrets) == 0 {
		return values
	}
	out := maps.Clone(values)
	keyring := s.keyringNow()
	for _, key := range g.secrets {
		raw, ok := out[key].(string)
		if !ok || !secret.Encrypted(raw) {
			continue
		}
		plain, err := keyring.Unseal(raw)
		if err != nil {
			if logger := s.loggerNow(); logger != nil {
				logger.Warn("设置里的口令解不开，按未设置处理，请在后台重新填写",
					slog.String("group", g.Name), slog.String("field", key), slog.Any("error", err))
			}
			out[key] = ""
			continue
		}
		out[key] = plain
	}
	return out
}

// keyringNow 取当前主密钥；未注入时为 nil。
func (s *Service) keyringNow() *secret.Keyring {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keyring
}

// loggerNow 取当前日志器；未注入时为 nil。
func (s *Service) loggerNow() *slog.Logger {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logger
}

// Mask 抹掉口令字段的值，返回抹过的副本与其中确有值的字段名。
//
// 「确有值」这件事必须单独告诉界面：抹完之后每个口令字段都是空串，
// 而站长需要知道库里到底存没存过——否则他无从判断要不要重填。
func (g *Group) Mask(values map[string]any) (masked map[string]any, set []string) {
	if len(g.secrets) == 0 {
		return values, nil
	}
	set = make([]string, 0, len(g.secrets))
	masked = maps.Clone(values)
	for _, key := range g.secrets {
		if plain, ok := masked[key].(string); ok && plain != "" {
			set = append(set, key)
		}
		masked[key] = ""
	}
	return masked, set
}

// RejectSecrets 拒绝在作用域设置（主题、插件）里声明的口令字段。
//
// 那些设置的存法与站点设置不同，加解密没有接进去；放任它们声明 Secret
// 会得到一个「界面上写着不回传、库里其实躺着明文」的假象，比直接不支持更糟。
func RejectSecrets(scope string, f *form.Form) error {
	if f == nil {
		return nil
	}
	if keys := f.SecretKeys(); len(keys) > 0 {
		return fmt.Errorf("%s 不支持口令字段 %s：作用域设置不进加密存储，请改用环境变量或站点设置",
			scope, strings.Join(keys, "、"))
	}
	return nil
}

// Public 返回各分组中标记为公开的字段，供前台与登录页使用。
func (s *Service) Public(ctx context.Context) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for _, g := range s.Groups() {
		if len(g.Public) == 0 {
			continue
		}
		values, err := s.Effective(ctx, g.Name)
		if err != nil {
			return nil, err
		}
		picked := make(map[string]any, len(g.Public))
		for _, key := range g.Public {
			if v, ok := values[key]; ok {
				picked[key] = v
			}
		}
		out[g.Name] = picked
	}
	return out, nil
}

// Slug 按站点设置的策略生成 slug：unicode 保留中文，pinyin 转为拼音。
//
// 分组尚未注册或读取失败时退回 unicode 策略，不让一次设置读取失败拖垮内容创建。
func (s *Service) Slug(ctx context.Context, text string) string {
	var site Site
	if err := s.Get(ctx, GroupSite, &site); err == nil && site.SlugStrategy == SlugPinyin {
		return slug.Pinyin(text)
	}
	return slug.Make(text)
}

// merge 返回 defaults 被 overrides 按顶层键覆盖后的新对象。
func merge(defaults, overrides map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(overrides))
	maps.Copy(out, defaults)
	maps.Copy(out, overrides)
	return out
}
