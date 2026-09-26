package theme

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
)

// 前台共享缓存的时限。
const (
	// sharedTTL 是一条缓存的最长寿命：多实例部署时，别的实例的写入最迟这么久后可见（与设置服务的缓存一致）。
	sharedTTL = 30 * time.Second
	// sharedQuiet 是一次写入之后不再存缓存的时长：写语句执行时事务往往还没提交，
	// 这期间读到的是旧数据，存下来就要旧到下一次写入。
	sharedQuiet = 2 * time.Second
	// sharedMaxEntries 是条目上限，满了整个清空：键里带文章 ID，访问过的文章越多条目越多。
	sharedMaxEntries = 4096
)

// sharedCache 是前台查询的跨请求缓存。
//
// 失效靠写入而不是时限：本进程里任何一条写进内容相关表的语句都会清空它（见 writeHook），
// 编辑改完文章、菜单、主题设置，刷新前台立刻看到。只缓存与访客无关的数据，
// 「当前用户收藏过没有」、作者预览自己的私密文章这类都不进来。
type sharedCache struct {
	mu      sync.Mutex
	entries map[string]sharedEntry
	// epoch 每次失效加一：查询开始前后 epoch 变了，说明查询期间有写入，结果不存。
	epoch uint64
	// quietUntil 之前不存缓存，见 sharedQuiet。
	quietUntil time.Time
	now        func() time.Time
}

type sharedEntry struct {
	value   any
	expires time.Time
}

func newSharedCache() *sharedCache {
	return &sharedCache{entries: map[string]sharedEntry{}, now: time.Now}
}

// get 取一条未过期的缓存；没有时同时返回当前的 epoch，供查询完成后 put 比对。
func (c *sharedCache) get(key string) (value any, epoch uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expires) {
		return nil, c.epoch, false
	}
	return entry.value, c.epoch, true
}

// put 存下一次查询的结果；查询期间发生过写入、或还在写入后的静默期内时放弃。
func (c *sharedCache) put(key string, value any, epoch uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if epoch != c.epoch || now.Before(c.quietUntil) {
		return
	}
	if len(c.entries) >= sharedMaxEntries {
		clear(c.entries)
	}
	c.entries[key] = sharedEntry{value: value, expires: now.Add(sharedTTL)}
}

// invalidate 清空缓存并进入静默期。
func (c *sharedCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.epoch++
	c.quietUntil = c.now().Add(sharedQuiet)
}

// remember 先查共享缓存，没有再调 build 并存下；c 为 nil 时直接调 build。
// 出错的结果不存。
func remember[T any](c *sharedCache, key string, build func() (T, error)) (T, error) {
	if c == nil {
		return build()
	}
	v, epoch, ok := c.get(key)
	if ok {
		if typed, ok := v.(T); ok {
			return typed, nil
		}
	}
	value, err := build()
	if err == nil {
		c.put(key, value, epoch)
	}
	return value, err
}

// writeHook 在写进内容相关表的语句执行后清空前台缓存。
//
// 挂在数据库层而不是各个写入口：后台接口、定时发布、插件经宿主写内容、评论提交都走这里，
// 以后新加的写入口也不会漏。
type writeHook struct{ cache *sharedCache }

func (writeHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }

func (h writeHook) AfterQuery(_ context.Context, e *bun.QueryEvent) {
	if e.Err == nil && touchesContent(e.Query) {
		h.cache.invalidate()
	}
}

// ignoredTables 是写得频繁、又与前台渲染无关的表：会话、限流、任务认领、插件自己的数据、搜索索引。
// 名单之外的表一律按「可能影响前台」处理，新加的表默认会让缓存失效，宁可多清一次。
var ignoredTables = map[string]bool{
	"sessions":       true,
	"access_tokens":  true,
	"account_tokens": true,
	"rate_limits":    true,
	"job_runs":       true,
	"plugin_kv":      true,
	"extensions":     true,
	"post_search":    true,
}

var (
	// notWrites 是含 UPDATE 字样却不写别的表的子句，先抹掉再找写入目标。
	notWrites = regexp.MustCompile(`(?i)\bdo\s+update\b|\bfor\s+(?:no\s+key\s+)?update\b`)
	// writeTargets 取出写语句的目标表名，覆盖 bun 生成的带引号写法与手写 SQL。
	writeTargets = regexp.MustCompile(`(?i)\b(?:insert\s+into|update|delete\s+from|truncate(?:\s+table)?|merge\s+into)\s+(?:only\s+)?"?([a-z_][a-z0-9_]*)"?`)
)

// touchesContent 判断一条语句是否写了名单外的表。
func touchesContent(query string) bool {
	for _, m := range writeTargets.FindAllStringSubmatch(notWrites.ReplaceAllString(query, ""), -1) {
		if !ignoredTables[strings.ToLower(m[1])] {
			return true
		}
	}
	return false
}
