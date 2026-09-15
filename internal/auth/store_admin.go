package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/database"
)

// 用户与角色管理补充的错误哨兵。ErrNotFound / ErrDuplicate / ErrBuiltinRole
// 已在 store.go 中定义，这里不重复。
var (
	// ErrUserNotFound 表示用户不存在。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrRoleNotFound 表示角色不存在。
	ErrRoleNotFound = errors.New("角色不存在")
	// ErrRoleInUse 表示角色仍被用户持有，不能删除。
	ErrRoleInUse = errors.New("角色仍在使用中")
	// ErrLastAdmin 表示该操作会让站点失去最后一名管理员。
	ErrLastAdmin = errors.New("不能移除最后一名管理员")
	// ErrSelfOperation 表示该操作不允许作用在自己身上。
	ErrSelfOperation = errors.New("不能对自己执行该操作")
	// ErrUserHasContent 表示用户仍拥有内容，不能删除。
	ErrUserHasContent = errors.New("用户仍有内容")
	// ErrRoleNotBuiltin 表示对自定义角色执行了只有内置角色才有的操作（恢复默认）。
	ErrRoleNotBuiltin = errors.New("自定义角色没有可恢复的默认权限")
)

// asConstraintViolation 暴露给本包内部的约束冲突识别。
func asConstraintViolation(err error) (*database.ConstraintViolation, bool) {
	return database.AsConstraintViolation(err)
}

// UserFilter 是用户列表的筛选条件。
type UserFilter struct {
	// Role 非空时只返回拥有该角色的用户。
	Role string
	// Status 取 enabled 或 disabled。
	Status string
	// Q 非空时按用户名、邮箱或显示名模糊筛选。
	Q string
}

// 用户状态筛选取值。
const (
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
)

// PageUsers 分页返回用户（含角色），按创建时间倒序。
func (s *Store) PageUsers(ctx context.Context, filter UserFilter, params api.PageParams) ([]User, int, error) {
	out := []User{}
	q := s.db.NewSelect().Model(&out).Order("u.created_at DESC", "u.id DESC")

	if filter.Role != "" {
		q = q.Where("EXISTS (SELECT 1 FROM user_roles AS ur JOIN roles AS r ON r.id = ur.role_id "+
			"WHERE ur.user_id = u.id AND r.name = ?)", filter.Role)
	}
	switch filter.Status {
	case StatusEnabled:
		q = q.Where("u.disabled = false")
	case StatusDisabled:
		q = q.Where("u.disabled = true")
	}
	if text := strings.TrimSpace(filter.Q); text != "" {
		pattern := "%" + escapeLike(text) + "%"
		q = q.Where("(u.username ILIKE ? OR u.email ILIKE ? OR u.display_name ILIKE ?)",
			pattern, pattern, pattern)
	}

	total, err := q.Limit(params.Limit()).Offset(params.Offset()).ScanAndCount(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("分页查询用户: %w", err)
	}
	if err := s.attachRoles(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// FindUsersByIDs 的补充：批量填充角色，避免列表接口的 N+1 查询。
func (s *Store) attachRoles(ctx context.Context, users []User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(users))
	for i := range users {
		ids = append(ids, users[i].ID)
	}

	var links []UserRole
	if err := s.db.NewSelect().Model(&links).Where("ur.user_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return fmt.Errorf("查询用户角色关联: %w", err)
	}
	roleIDs := make([]int64, 0, len(links))
	for _, l := range links {
		roleIDs = append(roleIDs, l.RoleID)
	}

	roles := map[int64]Role{}
	if len(roleIDs) > 0 {
		var rows []Role
		if err := s.db.NewSelect().Model(&rows).Where("r.id IN (?)", bun.List(uniqueIDs(roleIDs))).
			Order("r.name").Scan(ctx); err != nil {
			return fmt.Errorf("查询角色: %w", err)
		}
		for i := range rows {
			roles[rows[i].ID] = rows[i]
		}
	}

	byUser := map[int64][]Role{}
	for _, l := range links {
		if role, ok := roles[l.RoleID]; ok {
			byUser[l.UserID] = append(byUser[l.UserID], role)
		}
	}
	for i := range users {
		users[i].Roles = byUser[users[i].ID]
		if users[i].Roles == nil {
			users[i].Roles = []Role{}
		}
	}
	return nil
}

// GetUser 按 ID 查用户并加载角色。
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	u, err := s.FindUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

// UpdateUserProfile 更新用户的展示信息，不动口令与角色。
func (s *Store) UpdateUserProfile(ctx context.Context, userID int64, email, displayName, avatarURL, bio string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	res, err := s.db.NewUpdate().Model((*User)(nil)).
		Set("email = ?", email).
		Set("display_name = ?", strings.TrimSpace(displayName)).
		Set("avatar_url = ?", strings.TrimSpace(avatarURL)).
		Set("bio = ?", strings.TrimSpace(bio)).
		Set("updated_at = now()").
		Where("id = ?", userID).
		Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: 邮箱已被占用", ErrDuplicate)
		}
		return fmt.Errorf("更新用户: %w", err)
	}
	return requireOneRow(res, "用户")
}

// CountAdmins 统计拥有管理员角色的用户数。
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	n, err := s.db.NewSelect().Model((*User)(nil)).
		Where("u.disabled = false").
		Where("EXISTS (SELECT 1 FROM user_roles AS ur JOIN roles AS r ON r.id = ur.role_id "+
			"WHERE ur.user_id = u.id AND r.name IN (?))", bun.List([]string{perm.RoleSuperAdmin, perm.RoleAdmin})).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("统计管理员: %w", err)
	}
	return n, nil
}

// DeleteUser 删除用户及其角色关联。
//
// 用户发布过的内容通过外键 RESTRICT 阻止删除：那些内容仍需要作者。
// 需要保留内容时正确做法是停用账号，而不是删人。
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return s.runInTx(ctx, func(tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*UserRole)(nil)).Where("user_id = ?", id).Exec(ctx); err != nil {
			return fmt.Errorf("清除用户角色: %w", err)
		}
		res, err := tx.NewDelete().Model((*User)(nil)).Where("id = ?", id).Exec(ctx)
		if err != nil {
			if v, ok := asConstraintViolation(err); ok && v.Code == "23503" {
				return fmt.Errorf("%w：该用户仍有内容，请改为停用", ErrUserHasContent)
			}
			return fmt.Errorf("删除用户: %w", err)
		}
		return requireOneRow(res, "用户")
	})
}

// HasAdminRole 报告用户的角色集合里是否含管理员角色。
func HasAdminRole(roleNames []string) bool {
	for _, name := range roleNames {
		if name == perm.RoleAdmin || name == perm.RoleSuperAdmin {
			return true
		}
	}
	return false
}

// CreateRole 创建自定义角色。
func (s *Store) CreateRole(ctx context.Context, role *Role) error {
	role.Builtin = false
	now := time.Now()
	role.CreatedAt, role.UpdatedAt = now, now
	if role.Permissions == nil {
		role.Permissions = []perm.Permission{}
	}
	if _, err := s.db.NewInsert().Model(role).Returning("*").Exec(ctx); err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: 角色名已被占用", ErrDuplicate)
		}
		return fmt.Errorf("创建角色: %w", err)
	}
	return nil
}

// UpdateRole 更新角色的显示名、描述与权限。
//
// 内置角色自 2026-09-15 起可改：三挡角色（用户 / 编辑 / 管理员）的权限由站长决定，
// 名字仍是不可改的键。super-admin 整条锁定 —— 把它的权限改坏等于拆掉唯一的后门。
func (s *Store) UpdateRole(ctx context.Context, roleID int64, label, description string, permissions []perm.Permission) error {
	current, err := s.roleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if current.Locked {
		return ErrRoleLocked
	}
	if permissions == nil {
		permissions = []perm.Permission{}
	}
	res, err := s.db.NewUpdate().Model((*Role)(nil)).
		Set("label = ?", strings.TrimSpace(label)).
		Set("description = ?", strings.TrimSpace(description)).
		Set("permissions = ?", permissions).
		Set("updated_at = now()").
		Where("id = ?", roleID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新角色: %w", err)
	}
	return requireOneRow(res, "角色")
}

// ResetRole 把内置角色的权限、显示名与描述恢复成代码里的默认值。
//
// 存在的理由：内置角色的权限只在首次创建时播种（见 SeedRoles），升级新增的权限
// 不会自动补上。有这一条，「升级后编辑角色缺了新权限」才有自助解法，
// 而不是要站长去翻本文档再逐条勾回来。
func (s *Store) ResetRole(ctx context.Context, roleID int64) error {
	current, err := s.roleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if current.Locked {
		return ErrRoleLocked
	}
	defaults, ok := perm.BuiltinRoles[current.Name]
	if !ok {
		return ErrRoleNotBuiltin
	}
	meta := perm.BuiltinRoleMeta[current.Name]
	res, err := s.db.NewUpdate().Model((*Role)(nil)).
		Set("label = ?", meta.Label).
		Set("description = ?", meta.Description).
		Set("permissions = ?", defaults).
		Set("updated_at = now()").
		Where("id = ?", roleID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("恢复默认角色: %w", err)
	}
	return requireOneRow(res, "角色")
}

// DeleteRole 删除自定义角色；仍被用户持有时拒绝。
func (s *Store) DeleteRole(ctx context.Context, roleID int64) error {
	current, err := s.roleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if current.Builtin {
		return ErrBuiltinRole
	}

	holders, err := s.db.NewSelect().Model((*UserRole)(nil)).Where("role_id = ?", roleID).Count(ctx)
	if err != nil {
		return fmt.Errorf("统计角色持有者: %w", err)
	}
	if holders > 0 {
		return fmt.Errorf("%w：仍有 %d 名用户持有该角色", ErrRoleInUse, holders)
	}

	res, err := s.db.NewDelete().Model((*Role)(nil)).Where("id = ?", roleID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("删除角色: %w", err)
	}
	return requireOneRow(res, "角色")
}

// roleByID 按 ID 查角色。
func (s *Store) roleByID(ctx context.Context, id int64) (*Role, error) {
	role := new(Role)
	if err := s.db.NewSelect().Model(role).Where("r.id = ?", id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, fmt.Errorf("查询角色: %w", err)
	}
	role.fillMeta()
	return role, nil
}

// RoleByName 按名称查角色。
func (s *Store) RoleByName(ctx context.Context, name string) (*Role, error) {
	role, err := s.FindRoleByName(ctx, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

// uniqueIDs 去重，保持首次出现的顺序。
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// escapeLike 转义 LIKE 模式中的通配符，让用户输入按字面匹配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
