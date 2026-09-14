package favorite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// 错误哨兵。处理器据此映射为 404。
var (
	// ErrPostNotFound 表示要收藏的内容不存在，或已不对访客公开。
	//
	// 两种情况合成一个错误是有意的：收藏接口对「草稿」与「不存在」
	// 必须给出同一个回答，否则它就成了一个探测未发布内容是否存在的接口。
	ErrPostNotFound = errors.New("收藏的内容不存在")
)

// 内容可见性的取值。与 content 模块的常量同义，但这里不引那个包：
// 模块之间只允许经 App 交互（与 comment 的 Store 同一处理），
// 两个字符串不值得开一个依赖。
const (
	postStatusPublished  = "published"
	postVisibilityPublic = "public"
)

// Favorite 是一条收藏记录。
//
// 没有 json 标签：收藏不经 Console 平面，也没有任何接口回传整行——
// 对外只回答「收没收」与「多少人收了」两件事（见 handler.go）。
type Favorite struct {
	bun.BaseModel `bun:"table:favorites,alias:fv"`

	ID        int64     `bun:"id,pk,autoincrement"`
	UserID    int64     `bun:"user_id,notnull"`
	PostID    int64     `bun:"post_id,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero"`
}

// Store 提供收藏的持久化操作。
//
// 直接用 bun 读 posts 表而不经 content 模块：两者共用同一张表，
// 而模块之间只允许经 App 交互，不值得为一次可见性判定引入跨模块依赖
// （与 comment 的 Store 同一条理由）。
type Store struct {
	db *bun.DB
}

// NewStore 构造 Store。
func NewStore(db *bun.DB) *Store { return &Store{db: db} }

// Add 收藏一篇内容，返回本次是否真的新增了记录。
//
// 幂等：已收藏时什么都不做并返回 false，而不是报「已存在」。
// 判重交给唯一约束（ON CONFLICT DO NOTHING）而不是先查后插——
// 用户连点两下发出的两个请求之间没有空隙可钻，而先查后插有。
func (s *Store) Add(ctx context.Context, userID, postID int64) (bool, error) {
	row := &Favorite{UserID: userID, PostID: postID, CreatedAt: time.Now()}
	res, err := s.db.NewInsert().Model(row).
		On("CONFLICT (user_id, post_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("收藏内容: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		// 驱动不报告影响行数时按「没出错就算成功」处理：
		// 收藏的最终状态由调用方随后读出的计数与标记决定，不靠这个返回值。
		return true, nil //nolint:nilerr // 影响行数只用于区分「新增」与「已存在」，两者都不是失败
	}
	return affected > 0, nil
}

// Remove 取消收藏，返回本次是否真的删掉了记录。
//
// 同样幂等：没收藏过也算成功。前端的按钮状态可能与库里不一致
// （换了设备、在另一个标签页里点过），把这种情况当错误报出来毫无意义。
func (s *Store) Remove(ctx context.Context, userID, postID int64) (bool, error) {
	res, err := s.db.NewDelete().Model((*Favorite)(nil)).
		Where("user_id = ? AND post_id = ?", userID, postID).
		Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("取消收藏: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return true, nil //nolint:nilerr // 同 Add：影响行数只用于区分两种成功
	}
	return affected > 0, nil
}

// VisiblePost 校验内容存在且对访客可见，不可见时返回 ErrPostNotFound。
//
// 判据与主题前台的 publicFilter、评论接口的 PubliclyVisible 完全一致：已发布且公开。
// 作者本人的私密内容也不允许收藏——收藏列表是一张长期存在的清单，
// 而内容的可见性随时会变，列表页每次都要再过一遍可见性（见 FavoritePostIDs），
// 那里没有「作者可见自己的私密内容」这一路，写入时也就不该放行。
func (s *Store) VisiblePost(ctx context.Context, postID int64) error {
	var status, visibility string
	err := s.db.NewRaw("SELECT status, visibility FROM posts WHERE id = ?", postID).
		Scan(ctx, &status, &visibility)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPostNotFound
	}
	if err != nil {
		return fmt.Errorf("查询收藏目标: %w", err)
	}
	if status != postStatusPublished || visibility != postVisibilityPublic {
		return ErrPostNotFound
	}
	return nil
}

// ---------- 供主题前台取数的窄接口 ----------
//
// 以下三个方法是 theme 的 Favoriter 接口（internal/theme/store.go）的实现。
// 方法名带 Favorite 前缀是因为在调用方那里它们与 SearchPostIDs 并列，
// 光看 Has 与 Count 读不出问的是什么——与 search.Service.SearchPostIDs 同一条命名理由。

// HasFavorite 报告某人是否收藏过某篇内容。
func (s *Store) HasFavorite(ctx context.Context, userID, postID int64) (bool, error) {
	if userID <= 0 || postID <= 0 {
		return false, nil
	}
	ok, err := s.db.NewSelect().Model((*Favorite)(nil)).
		Where("user_id = ? AND post_id = ?", userID, postID).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("查询收藏状态: %w", err)
	}
	return ok, nil
}

// CountFavorites 返回一篇内容被收藏的次数。
func (s *Store) CountFavorites(ctx context.Context, postID int64) (int, error) {
	if postID <= 0 {
		return 0, nil
	}
	n, err := s.db.NewSelect().Model((*Favorite)(nil)).
		Where("post_id = ?", postID).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("统计收藏数: %w", err)
	}
	return n, nil
}

// FavoritePostIDs 返回某人收藏的、当前仍对访客可见的内容 ID，按收藏时间倒序，
// 另返回符合条件的总数。
//
// 只出 ID 不出内容：作者、分类、标签与列表视图由主题自己的查询补齐，
// 与 search.Service.SearchPostIDs 同一种分工——收藏模块不必知道前台怎么展示一篇文章。
//
// 可见性在这里再过一遍（而不是只在写入时过一遍）：一篇文章被收藏之后
// 完全可能被撤回或转为私密，那时它必须从收藏页上消失，
// 否则收藏页就成了一扇能看见已下线内容标题的窗。
// 总数与列表用同一个 WHERE，故「显示 3 条却说共 5 篇」不会发生。
func (s *Store) FavoritePostIDs(ctx context.Context, userID int64, limit, offset int) (
	ids []int64, total int, err error,
) {
	if userID <= 0 || limit <= 0 {
		return nil, 0, nil
	}

	const where = `fv.user_id = ? AND p.status = ? AND p.visibility = ?`
	args := []any{userID, postStatusPublished, postVisibilityPublic}

	total, err = s.db.NewSelect().Model((*Favorite)(nil)).
		Join("JOIN posts AS p ON p.id = fv.post_id").
		Where(where, args...).
		Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("统计收藏: %w", err)
	}
	if total == 0 || offset >= total {
		return nil, total, nil
	}

	ids = []int64{}
	err = s.db.NewSelect().Model((*Favorite)(nil)).
		Column("fv.post_id").
		Join("JOIN posts AS p ON p.id = fv.post_id").
		Where(where, args...).
		Order("fv.created_at DESC", "fv.id DESC").
		Limit(limit).Offset(offset).
		Scan(ctx, &ids)
	if err != nil {
		return nil, 0, fmt.Errorf("查询收藏: %w", err)
	}
	return ids, total, nil
}
