package menu

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/uptrace/bun"
)

// wrapTargetError 把「查无此行」统一成本包的哨兵错误，其余错误原样返回。
func wrapTargetError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTargetNotFound
	}
	return err
}

// isMissingTarget 报告错误是否表示目标记录已不存在。
func isMissingTarget(err error) bool {
	return errors.Is(err, ErrTargetNotFound) || errors.Is(err, sql.ErrNoRows)
}

// 前台路径前缀，与主题路由约定一致。
const (
	pathPosts      = "/posts/"
	pathPages      = "/"
	pathCategories = "/categories/"
	pathTags       = "/tags/"
)

// resolver 把站内条目解析为真实地址。
//
// 直接用 bun 读其它模块的表：菜单只关心「这条记录还在不在、地址是什么」，
// 为此引入对 content / taxonomy 的编译期依赖并不划算，跨模块读到的是同一张表。
type resolver struct {
	db *bun.DB
}

// resolve 解析一个站内条目的地址。
//
// 记录已被删除时返回 ErrTargetNotFound：前台渲染会跳过该条目，
// 而不是留下一串点了就 404 的死链。
func (r *resolver) resolve(ctx context.Context, item *Item) error {
	if item.Type == TypeCustom {
		return nil
	}
	if item.TargetID == nil {
		return fmt.Errorf("%w：%s 类型缺少 targetId", ErrInvalid, item.Type)
	}

	switch item.Type {
	case TypePost, TypePage:
		var row struct {
			Slug   string `bun:"slug"`
			Type   string `bun:"type"`
			Status string `bun:"status"`
		}
		err := r.db.NewRaw(
			"SELECT slug, type, status FROM posts WHERE id = ?", *item.TargetID).Scan(ctx, &row)
		if err != nil {
			return wrapTargetError(fmt.Errorf("查询菜单条目目标: %w", err))
		}
		// 未发布的内容不该出现在菜单里。
		if row.Status != "published" {
			return ErrTargetNotFound
		}
		if row.Type == "page" {
			item.URL = pathPages + row.Slug
		} else {
			item.URL = pathPosts + row.Slug
		}
		return nil

	case TypeCategory:
		var slug string
		if err := r.db.NewRaw("SELECT slug FROM categories WHERE id = ?", *item.TargetID).Scan(ctx, &slug); err != nil {
			return wrapTargetError(fmt.Errorf("查询菜单条目目标: %w", err))
		}
		item.URL = pathCategories + slug
		return nil

	case TypeTag:
		var slug string
		if err := r.db.NewRaw("SELECT slug FROM tags WHERE id = ?", *item.TargetID).Scan(ctx, &slug); err != nil {
			return wrapTargetError(fmt.Errorf("查询菜单条目目标: %w", err))
		}
		item.URL = pathTags + slug
		return nil
	}
	return fmt.Errorf("%w：未知的条目类型 %q", ErrInvalid, item.Type)
}

// resolveBestEffort 尽力解析站内条目的地址，解析不了的保留原文。
//
// 后台要看到全部条目（包括指向已删除记录的那些）才能修，故只补地址、不剔除。
func (r *resolver) resolveBestEffort(ctx context.Context, items []Item) {
	for i := range items {
		if err := r.resolve(ctx, &items[i]); err != nil {
			items[i].URL = ""
		}
	}
}

// resolveAll 解析全部站内条目；指向已删除记录的条目被就地剔除并返回数量。
func (r *resolver) resolveAll(ctx context.Context, items []Item) ([]Item, int, error) {
	kept := make([]Item, 0, len(items))
	dropped := 0
	for i := range items {
		item := items[i]
		err := r.resolve(ctx, &item)
		if err == nil {
			kept = append(kept, item)
			continue
		}
		if isMissingTarget(err) {
			dropped++
			continue
		}
		return nil, 0, err
	}
	return kept, dropped, nil
}
