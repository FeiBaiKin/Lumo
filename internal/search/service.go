package search

import (
	"context"
	"log/slog"
	"time"

	"github.com/FeiBaiKin/lumo/internal/api"
)

// 对账节奏。
const (
	// reconcileInterval 是两轮对账之间的间隔，也就是新内容进入搜索的最大延迟。
	reconcileInterval = 2 * time.Second
	// reconcileBatch 是单次取出的待索引条数。
	reconcileBatch = 200
	// reconcileMaxBatches 是单轮最多处理的批数。
	//
	// 批量导入时一轮就把积压吃完，而不是每 2 秒才前进 200 条；
	// 又不至于在持续写入时把一轮变成无限循环。
	reconcileMaxBatches = 25
)

// Params 是一次搜索的入参。
type Params struct {
	// Query 是用户输入的原始关键词。
	Query string
	// Types 限定内容类型（post / page）；为空表示不限。
	Types []string
	Page  api.PageParams
}

// Result 是一次搜索的结果。
type Result struct {
	Hits  []Hit
	Total int
}

// Searcher 是搜索能力的抽象。
//
// 前台与接口都只依赖这个接口，将来换成 meilisearch 一类外部引擎时，
// 换掉实现即可，调用方不动。
type Searcher interface {
	Search(ctx context.Context, params *Params) (*Result, error)
}

// Service 是基于 PostgreSQL tsvector 的搜索实现。
type Service struct {
	store  *Store
	logger *slog.Logger
}

// 编译期确认 Service 满足对外接口。
var _ Searcher = (*Service)(nil)

// NewService 构造 Service。
func NewService(store *Store, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// Search 按相关度返回命中的内容；关键词切不出任何词元时返回空结果而不是报错。
func (s *Service) Search(ctx context.Context, params *Params) (*Result, error) {
	tsquery := TSQuery(params.Query)
	if tsquery == "" {
		return &Result{Hits: []Hit{}}, nil
	}

	page := params.Page.Normalize()
	hits, total, err := s.store.Search(ctx, tsquery, params.Types, page.Limit(), page.Offset())
	if err != nil {
		return nil, err
	}
	return &Result{Hits: hits, Total: total}, nil
}

// SearchPostIDs 按相关度返回文章 ID 与总数，供主题前台取回完整视图。
//
// 实现 theme.Searcher：前台只要「哪些、什么顺序」，作者与分类标签由主题自己补齐。
// 固定只搜文章——搜索页展示的是文章流，独立页面（关于、联系我们）混进去只会干扰；
// 要搜页面的调用方走 Public 接口的 type 参数。
func (s *Service) SearchPostIDs(ctx context.Context, query string, limit, offset int) (
	ids []int64, total int, err error,
) {
	tsquery := TSQuery(query)
	if tsquery == "" {
		return nil, 0, nil
	}
	limit = min(max(limit, 1), api.MaxPageSize)
	offset = max(offset, 0)

	hits, total, err := s.store.Search(ctx, tsquery, []string{"post"}, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	ids = make([]int64, 0, len(hits))
	for i := range hits {
		ids = append(ids, hits[i].ID)
	}
	return ids, total, nil
}

// Reindex 把全部内容标记为待索引，由后台对账逐批重建，返回待重建的条数。
func (s *Service) Reindex(ctx context.Context) (int, error) {
	return s.store.MarkAllStale(ctx)
}

// Stats 返回索引进度。
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	return s.store.Stats(ctx)
}

// Run 持续把过期的索引补齐，直到 ctx 取消。
//
// 不在内容模块的写路径上挂钩子，而是按 posts.updated_at 对账：写路径有七八条
// （创建、改稿、发布、撤回、进回收站、恢复、定时发布扫描……），将来还会加，
// 漏挂一处的表现是「那部分内容永远搜不到」且没有任何报错。对账则天然覆盖全部写法，
// 连批量导入与直接改库都跟得上，代价只是最多 reconcileInterval 的延迟。
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		s.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reconcile 处理一轮积压，最多 reconcileMaxBatches 批。
func (s *Service) reconcile(ctx context.Context) {
	for range reconcileMaxBatches {
		rows, err := s.store.Stale(ctx, reconcileBatch)
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Warn("查询待索引内容失败", slog.Any("error", err))
			}
			return
		}
		if len(rows) == 0 {
			return
		}

		var failed int
		var firstErr error
		for i := range rows {
			if indexErr := s.store.Index(ctx, &rows[i]); indexErr != nil {
				failed++
				if firstErr == nil {
					firstErr = indexErr
				}
			}
		}
		// 逐条报会刷屏：失败的行下一轮还会被取出来，错误是重复的。
		if firstErr != nil && ctx.Err() == nil {
			s.logger.Warn("部分内容建索引失败，下一轮重试",
				slog.Int("failed", failed), slog.Any("error", firstErr))
		}
		if ctx.Err() != nil {
			return
		}
		if len(rows) < reconcileBatch {
			return
		}
	}
}
