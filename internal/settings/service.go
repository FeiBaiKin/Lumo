package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/form"
	"github.com/FeiBaiKin/lumo/internal/httpx"
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
}

// SchemaDoc 返回解析后的 Schema 文档，供接口输出。
func (g *Group) SchemaDoc() map[string]any { return g.doc }

// Defaults 返回缺省值的副本。
func (g *Group) DefaultValues() map[string]any { return maps.Clone(g.defaults) }

type cacheEntry struct {
	values  map[string]any
	expires time.Time
}

// Service 是设置的读写入口：持有已注册分组、校验器与缓存。
type Service struct {
	store *Store

	mu     sync.RWMutex
	groups map[string]*Group
	order  []string
	cache  map[string]cacheEntry
}

// NewService 构造 Service；store 可为 nil（仅做 Schema 校验的场景）。
func NewService(store *Store) *Service {
	return &Service{store: store, groups: map[string]*Group{}, cache: map[string]cacheEntry{}}
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
	values := merge(g.defaults, stored)

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

// Update 用给定值覆盖分组：先与缺省值合并成完整对象，校验通过后整体保存并返回有效值。
func (s *Service) Update(ctx context.Context, name string, values map[string]any) (map[string]any, error) {
	g, ok := s.Group(name)
	if !ok {
		return nil, ErrUnknownGroup
	}
	effective := merge(g.defaults, values)
	if err := g.validate(effective); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, errors.New("settings: 未配置存储，无法写入")
	}
	if err := s.store.Save(ctx, name, effective); err != nil {
		return nil, err
	}

	s.mu.Lock()
	delete(s.cache, name)
	s.mu.Unlock()
	return maps.Clone(effective), nil
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
