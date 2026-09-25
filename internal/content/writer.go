package content

import (
	"context"
	"errors"
	"time"

	"github.com/FeiBaiKin/lumo/internal/hooks"
)

// ErrInvalidType 表示内容类型不是 post 或 page。
var ErrInvalidType = errors.New("内容类型只能是 post 或 page")

// Writer 是给插件这类内部调用方用的写入口。
//
// 规则与后台接口一致：正文渲染与净化（一律按没有 content:unsafe_html 的作者处理）、
// slug 生成与去重、动作派发都走同一套代码。权限由调用方判定——这里不认识调用方是谁。
type Writer struct{ h *Handler }

// WriteParams 是新建或修改一条内容的字段。
type WriteParams struct {
	Title string `json:"title"`
	// Slug 留空时新建按站点策略由标题生成、修改保留原值。
	Slug string `json:"slug"`
	// RawType 是原稿格式，缺省 html。
	RawType     RawType `json:"rawType"`
	Raw         string  `json:"raw"`
	Excerpt     string  `json:"excerpt"`
	CategoryIDs []int64 `json:"categoryIds"`
	TagIDs      []int64 `json:"tagIds"`
}

func kindOf(typ Type) (*kind, error) {
	for _, k := range kinds {
		if k.typ == typ {
			return k, nil
		}
	}
	return nil, ErrInvalidType
}

func (p *WriteParams) body() *body {
	return &body{
		Title: p.Title, Slug: p.Slug, RawType: p.RawType, Raw: p.Raw, Excerpt: p.Excerpt,
		CategoryIDs: p.CategoryIDs, TagIDs: p.TagIDs,
	}
}

// Create 新建一条内容，作者为 authorID；publish 为真时直接发布，否则存为草稿。
func (w *Writer) Create(ctx context.Context, typ Type, authorID int64, p *WriteParams, publish bool) (*Post, error) {
	k, err := kindOf(typ)
	if err != nil {
		return nil, err
	}
	post := &Post{Type: typ, Status: StatusDraft, AuthorID: authorID}
	b := p.body()
	if applyErr := applyBody(post, b, k, false); applyErr != nil {
		return nil, applyErr
	}
	if publish {
		now := time.Now()
		post.Status, post.PublishedAt = StatusPublished, &now
	}
	s, err := w.h.resolveSlug(ctx, b.Slug, post.Title, "")
	if err != nil {
		return nil, err
	}
	categories, tags := termsOf(k, b)
	err = withSlugRetry(isGenerated(b.Slug), s, func(candidate string) error {
		post.Slug = candidate
		return w.h.store.Create(ctx, post, categories, tags)
	})
	if err != nil {
		return nil, err
	}
	w.h.emit(ctx, hooks.PostUpdated, post)
	if publish {
		w.h.emit(ctx, hooks.PostPublished, post)
	}
	return post, nil
}

// Update 整体改写一条内容的标题与正文，不改变它的发布状态。editorID 记进修订历史。
func (w *Writer) Update(ctx context.Context, typ Type, id int64, p *WriteParams, editorID int64) (*Post, error) {
	k, err := kindOf(typ)
	if err != nil {
		return nil, err
	}
	post, err := w.h.store.Get(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	before := *post
	b := p.body()
	// 插件没给的封面、置顶、可见性与元数据保持原样：它改的是文字，不是整条内容
	b.CoverURL, b.Pinned, b.Visibility, b.Template, b.Meta = post.CoverURL, post.Pinned, post.Visibility, post.Template, post.Meta
	if applyErr := applyBody(post, b, k, false); applyErr != nil {
		return nil, applyErr
	}
	s, err := w.h.resolveSlug(ctx, b.Slug, post.Title, before.Slug)
	if err != nil {
		return nil, err
	}
	post.Slug = s
	categories, tags := termsOf(k, b)
	snapshot := before.Title != post.Title || before.Raw != post.Raw || before.RawType != post.RawType
	if err := w.h.store.Update(ctx, post, categories, tags, snapshot, editorID); err != nil {
		return nil, err
	}
	w.h.emit(ctx, hooks.PostUpdated, post)
	return post, nil
}

// Trash 把一条内容移入回收站。
func (w *Writer) Trash(ctx context.Context, typ Type, id int64) error {
	post, err := w.h.store.Get(ctx, typ, id)
	if err != nil {
		return err
	}
	now := time.Now()
	post.Status, post.TrashedAt = StatusTrashed, &now
	if err := w.h.store.UpdateStatus(ctx, post); err != nil {
		return err
	}
	w.h.emit(ctx, hooks.PostTrashed, post)
	return nil
}

// termsOf 取分类与标签：只有文章参与，页面一律忽略。
func termsOf(k *kind, b *body) (categories, tags []int64) {
	if !k.hasTerms {
		return nil, nil
	}
	return b.CategoryIDs, b.TagIDs
}
