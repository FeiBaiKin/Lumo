package auth

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// FindUsersByIDs 按 ID 批量查用户，不加载角色；供内容模块给文章附上作者信息。
//
// 返回映射而非切片：调用方按 ID 取用，不存在的 ID 不会出现在映射中。
func (s *Store) FindUsersByIDs(ctx context.Context, ids []int64) (map[int64]*User, error) {
	out := make(map[int64]*User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var users []User
	err := s.db.NewSelect().Model(&users).Where("u.id IN (?)", bun.List(ids)).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("批量查询用户: %w", err)
	}
	for i := range users {
		out[users[i].ID] = &users[i]
	}
	return out, nil
}
