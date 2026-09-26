// Package main 是示例插件「访问统计」。
//
// 它演示前台的另一条路：不动正文，而是在页尾带一个自己的脚本，脚本回打插件自己的公开接口，
// 后端把计数记进键值存储；侧栏小组件再把热门的几篇列出来。
//
// 只数已发布、公开的文章：私密文章的标题不该出现在前台的排行里。装插件之前就有的文章
// 在第一次被打开时经读内容的能力补登记，不必逐篇重新保存。
//
// 计数口径说清楚：一篇在一次会话里只计一次（去重在前台脚本里做），所以这是「会话数」而不是
// 「无重复访客数」——换个浏览器、清掉会话都会重新计数。要更准的口径得自己按 IP 或账号去重，
// 示例不做。
package main

import (
	"fmt"
	"html"
	"sort"
	"time"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

// config 是 settings.yaml 里 stats 分组的值。
type config struct {
	Track    bool `json:"track"`
	TopCount int  `json:"topCount"`
}

// postMeta 是每篇文章的题头信息，小组件要用。Path 为空表示这篇现在不该露面（进了回收站、
// 改成了私密或草稿）。
type postMeta struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// topEntry 是热门榜的一行。
type topEntry struct {
	ID    int64 `json:"id"`
	Views int64 `json:"views"`
}

const (
	// dailyPrefix 是每日计数的键前缀，保留 400 天。
	dailyPrefix = "daily:"
	dailyTTL    = 400 * 24 * time.Hour

	// topKey 存热门榜：按次数排的前 topSize 篇，每次计数时顺手更新。
	//
	// 小组件是访客打开页面时同步跑的，只能读一个键就出结果；逐篇的计数键可能成千上万，
	// 挨个读来排序既慢、又受 KV.List 单次上限所限。榜单与计数不在一个事务里：两个访客同时
	// 回打时，后写的会盖掉先写的那一笔名次更新，计数本身不丢，那篇下一次被打开就补回来。
	topKey  = "top"
	topSize = 50
)

func init() {
	lumo.OnRoute("hit", hit)
	lumo.OnPost(lumo.ActionPostPublished, remember)
	lumo.OnPost(lumo.ActionPostUpdated, remember)
	lumo.OnPost(lumo.ActionPostTrashed, forget)
	lumo.OnWidget("popular", popular)
	lumo.OnSlot(lumo.SlotContentAfter, views)
}

func main() {}

// views 是正文之后的插槽：这篇文章被打开过几次。
//
// 插槽与小组件一样是访客打开页面时同步跑的，所以只读一个键就返回，不做重活。
func views(_ *lumo.Context, page *lumo.PageInfo) (string, error) {
	if page.Post == nil {
		return "", nil
	}
	n, err := counter(page.Post.ID)
	if err != nil || n <= 0 {
		return "", err
	}
	return fmt.Sprintf(`<p class="visit-stats-count">本篇被打开过 <strong>%d</strong> 次。</p>`, n), nil
}

// hit 是公开接口：POST /api/v1/plugins/visit-stats/hit，body 为 {"post": 12}。
//
// 公开接口不认人，也没有 CSRF 令牌的要求，正适合这种由前台脚本发起的埋点。
// 只认站上已发布、公开的文章，其余 id 一律忽略：否则谁都能拿一堆假 id 把键值存储灌满。
func hit(_ *lumo.Context, req *lumo.Request) (*lumo.Response, error) {
	var in struct {
		Post int64 `json:"post"`
	}
	if err := req.Decode(&in); err != nil || in.Post <= 0 {
		return lumo.Problem(400, "请求体形如 {\"post\": 12}"), nil
	}
	cfg := loadConfig()
	if !cfg.Track {
		return lumo.NoContent(), nil
	}
	var meta postMeta
	known, err := lumo.KV.Get(metaKey(in.Post), &meta)
	if err != nil {
		return nil, err
	}
	if !known || meta.Path == "" {
		// 装插件之前就有的文章、或从回收站捞回来的，在这里补登记。读不到（不存在、
		// 宿主出错）与不该露面一样处理：这一下不计，也不给回打的脚本报错。
		if visible, err := sync(in.Post); err != nil || !visible {
			return lumo.NoContent(), nil
		}
	}
	n, err := lumo.KV.Incr(viewsKey(in.Post), 1, 0)
	if err != nil {
		return nil, err
	}
	if _, err := lumo.KV.Incr(dailyPrefix+day(), 1, dailyTTL); err != nil {
		return nil, err
	}
	if err := bump(in.Post, n); err != nil {
		return nil, err
	}
	return lumo.NoContent(), nil
}

// remember 在文章发布或更新时重新登记：标题可能改了，也可能改成了私密。
func remember(_ *lumo.Context, p *lumo.Post) error {
	if p.ID <= 0 {
		return nil
	}
	_, err := sync(p.ID)
	return err
}

// forget 在文章进回收站时把它从排行榜上撤下来，计数留着不动。
func forget(_ *lumo.Context, p *lumo.Post) error {
	return hide(p.ID)
}

// sync 按站上的现状登记一篇：已发布且公开的记下标题与路径，其余的撤出前台。
func sync(id int64) (visible bool, err error) {
	post, err := lumo.Content.Post(id)
	if err != nil {
		return false, err
	}
	if post.Status != "published" || post.Visibility != "public" {
		return false, hide(id)
	}
	return true, lumo.KV.Set(metaKey(id), postMeta{Title: post.Title, Path: post.Path}, 0)
}

// hide 让一篇不再在前台露面：题头与计数都留着，只清掉路径并撤出热门榜。
func hide(id int64) error {
	var meta postMeta
	found, err := lumo.KV.Get(metaKey(id), &meta)
	if err != nil {
		return err
	}
	if found && meta.Path != "" {
		meta.Path = ""
		if err := lumo.KV.Set(metaKey(id), meta, 0); err != nil {
			return err
		}
	}
	top, err := loadTop()
	if err != nil {
		return err
	}
	kept := make([]topEntry, 0, len(top))
	for _, e := range top {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(top) {
		return nil
	}
	return lumo.KV.Set(topKey, kept, 0)
}

// bump 把一篇的最新次数写进热门榜；够不上榜的不写，省一次存储。
func bump(id, n int64) error {
	top, err := loadTop()
	if err != nil {
		return err
	}
	found := false
	for i := range top {
		if top[i].ID == id {
			top[i].Views, found = n, true
			break
		}
	}
	if !found {
		if len(top) >= topSize && n <= top[len(top)-1].Views {
			return nil
		}
		top = append(top, topEntry{ID: id, Views: n})
	}
	sort.SliceStable(top, func(i, j int) bool { return top[i].Views > top[j].Views })
	if len(top) > topSize {
		top = top[:topSize]
	}
	return lumo.KV.Set(topKey, top, 0)
}

// popular 是侧栏小组件：按浏览次数列出前几篇。读热门榜一个键，再读上榜几篇的题头。
func popular(_ *lumo.Context, _ *lumo.PageInfo) (string, error) {
	cfg := loadConfig()
	want := cfg.TopCount
	if want <= 0 {
		want = 5
	}
	top, err := loadTop()
	if err != nil {
		return "", err
	}
	out := ""
	shown := 0
	for _, e := range top {
		if shown == want {
			break
		}
		var meta postMeta
		if found, err := lumo.KV.Get(metaKey(e.ID), &meta); err != nil || !found || meta.Path == "" {
			continue
		}
		out += fmt.Sprintf(`<li><a href="%s">%s</a> <span class="visit-count">%d</span></li>`,
			html.EscapeString(meta.Path), html.EscapeString(meta.Title), e.Views)
		shown++
	}
	if shown == 0 {
		return `<p class="visit-stats">还没有访问记录。</p>`, nil
	}
	return `<ul class="visit-stats">` + out + `</ul>`, nil
}

func loadTop() ([]topEntry, error) {
	var top []topEntry
	if _, err := lumo.KV.Get(topKey, &top); err != nil {
		return nil, err
	}
	return top, nil
}

// loadConfig 读设置；读不到就用缺省值。
func loadConfig() config {
	cfg := config{Track: true, TopCount: 5}
	if err := lumo.SettingsInto("stats", &cfg); err != nil {
		lumo.Warn("读设置失败，按缺省值统计", "error", err)
	}
	return cfg
}

// counter 读一篇文章的次数；键不存在表示 0，不是错。
func counter(id int64) (int64, error) {
	var n int64
	if _, err := lumo.KV.Get(viewsKey(id), &n); err != nil {
		return 0, err
	}
	return n, nil
}

func metaKey(id int64) string  { return fmt.Sprintf("post:%d", id) }
func viewsKey(id int64) string { return fmt.Sprintf("views:%d", id) }
func day() string              { return time.Now().UTC().Format("2006-01-02") }
