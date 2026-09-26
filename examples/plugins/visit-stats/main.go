// Package main 是示例插件「访问统计」。
//
// 它演示前台的另一条路：不动正文，而是在页尾带一个自己的脚本，脚本回打插件自己的公开接口，
// 后端把计数记进键值存储；侧栏小组件再把热门的几篇列出来。
//
// 计数口径说清楚：一篇在一次会话里只计一次（去重在前台脚本里做），所以这是「会话数」而不是
// 「无重复访客数」——换个浏览器、清掉会话都会重新计数。要更准的口径得自己按 IP 或账号去重，
// 那要读内容的权限，示例不做。
package main

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	lumo "github.com/FeiBaiKin/lumo/sdk/go"
)

// config 是 settings.yaml 里 stats 分组的值。
type config struct {
	Track    bool `json:"track"`
	TopCount int  `json:"topCount"`
}

// postMeta 是每篇文章的题头信息，发布或更新时记下，小组件要用。
type postMeta struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

// daily 是每日计数的键前缀，保留 400 天。
const (
	dailyPrefix = "daily:"
	dailyTTL    = 400 * 24 * time.Hour
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
// 认不得的 id 直接忽略：否则谁都能拿一堆假 id 把键值存储灌满。
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
	known, err := lumo.KV.Get(metaKey(in.Post), &postMeta{})
	if err != nil {
		return nil, err
	}
	if !known {
		return lumo.NoContent(), nil
	}
	if _, err := lumo.KV.Incr(viewsKey(in.Post), 1, 0); err != nil {
		return nil, err
	}
	if _, err := lumo.KV.Incr(dailyPrefix+day(), 1, dailyTTL); err != nil {
		return nil, err
	}
	return lumo.NoContent(), nil
}

// remember 在文章发布或更新时记下标题与路径。
func remember(_ *lumo.Context, p *lumo.Post) error {
	if p.ID <= 0 {
		return nil
	}
	if _, err := lumo.KV.Incr(viewsKey(p.ID), 0, 0); err != nil {
		// 计数键要先存在，小组件列出来的才只含真正计过数的文章
		return err
	}
	return lumo.KV.Set(metaKey(p.ID), postMeta{Title: p.Title, Path: p.Path}, 0)
}

// forget 在文章进回收站时把它从排行榜上撤下来，计数留着不动。
func forget(_ *lumo.Context, p *lumo.Post) error {
	var meta postMeta
	if found, err := lumo.KV.Get(metaKey(p.ID), &meta); err != nil || !found {
		return err
	}
	meta.Path = ""
	return lumo.KV.Set(metaKey(p.ID), meta, 0)
}

// popular 是侧栏小组件：按浏览次数列出前几篇。
func popular(_ *lumo.Context, _ *lumo.PageInfo) (string, error) {
	cfg := loadConfig()
	want := cfg.TopCount
	if want <= 0 {
		want = 5
	}
	entries, err := lumo.KV.List("views:", 200)
	if err != nil {
		return "", err
	}
	type row struct {
		meta  postMeta
		views int64
	}
	rows := make([]row, 0, len(entries))
	for i := range entries {
		id, err := strconv.ParseInt(strings.TrimPrefix(entries[i].Key, "views:"), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		var views int64
		if err := entries[i].Decode(&views); err != nil || views <= 0 {
			continue
		}
		var meta postMeta
		if found, err := lumo.KV.Get(metaKey(id), &meta); err != nil || !found || meta.Path == "" {
			continue
		}
		rows = append(rows, row{meta: meta, views: views})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].views != rows[j].views {
			return rows[i].views > rows[j].views
		}
		return rows[i].meta.Title < rows[j].meta.Title
	})
	if len(rows) > want {
		rows = rows[:want]
	}
	if len(rows) == 0 {
		return `<p class="visit-stats">还没有访问记录。</p>`, nil
	}
	out := `<ul class="visit-stats">`
	for _, r := range rows {
		out += fmt.Sprintf(`<li><a href="%s">%s</a> <span class="visit-count">%d</span></li>`,
			html.EscapeString(r.meta.Path), html.EscapeString(r.meta.Title), r.views)
	}
	return out + `</ul>`, nil
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
