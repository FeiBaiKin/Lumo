package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/auth"
)

// userRow 是账户流程要看的 users 列。
//
// 不直接用 auth.User：那会把口令哈希一类与账户流程无关的字段一并读进来，
// 而本模块从头到尾不需要它们。少读一列就少一处泄漏面。
type userRow struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID              int64      `bun:"id,pk"`
	Email           string     `bun:"email"`
	EmailVerifiedAt *time.Time `bun:"email_verified_at"`
}

// Verified 报告邮箱是否已验证。
func (r *userRow) Verified() bool { return r.EmailVerifiedAt != nil }

// Store 提供账户流程需要的 users 表访问。
//
// 只读与只改两个字段：创建用户、改口令、踢会话这些一律走 auth.Store / auth.Service，
// 本模块不重复实现——那是「共用 users 表」这个决策的意义所在。
type Store struct {
	db bun.IDB
}

// NewStore 构造 Store。
func NewStore(db bun.IDB) *Store { return &Store{db: db} }

// FindByEmail 按邮箱查用户（大小写不敏感）。
//
// 邮箱在库里已由 auth.Store.CreateUser 归一化为小写，
// 这里同样先 lower 再比，避免大小写不同的同一个地址被当成两个账号。
func (s *Store) FindByEmail(ctx context.Context, email string) (*userRow, error) {
	needle := strings.TrimSpace(strings.ToLower(email))
	if needle == "" {
		return nil, auth.ErrNotFound
	}
	row := new(userRow)
	err := s.db.NewSelect().
		Model(row).
		Where("lower(u.email) = ?", needle).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, auth.ErrNotFound
		}
		return nil, fmt.Errorf("按邮箱查询用户: %w", err)
	}
	return row, nil
}

// FindVerifiedByEmail 按邮箱查一个**已验证**的用户；不存在或未验证都返回 auth.ErrNotFound。
//
// 找回密码用它而不是 FindByEmail：调用方要的正是「可以给他发重置链接」这个判断，
// 而区分「不存在」与「未验证」的返回值只会在调用处被写成 if，
// 不如把判断留在这一处。
func (s *Store) FindVerifiedByEmail(ctx context.Context, email string) (*userRow, error) {
	row, err := s.FindByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if !row.Verified() {
		return nil, auth.ErrNotFound
	}
	return row, nil
}

// MarkEmailVerified 把用户的邮箱标记为已验证。
//
// 用 coalesce 保证幂等：链接被点第二次（或用户开了两个标签页同时点）时
// 保留第一次的时间，不会把「什么时候验证的」改成一个更晚的值。
func (s *Store) MarkEmailVerified(ctx context.Context, userID int64) error {
	_, err := s.db.NewUpdate().
		Model((*userRow)(nil)).
		Set("email_verified_at = coalesce(email_verified_at, now())").
		Set("updated_at = now()").
		Where("id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("标记邮箱已验证: %w", err)
	}
	return nil
}
