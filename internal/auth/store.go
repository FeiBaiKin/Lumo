package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/auth/password"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// 错误哨兵。
var (
	// ErrNotFound 表示实体不存在。
	ErrNotFound = errors.New("对象不存在")
	// ErrDuplicate 表示唯一约束冲突（用户名或邮箱已存在）。
	ErrDuplicate = errors.New("对象已存在")
	// ErrBuiltinRole 表示试图删除内置角色。
	//
	// 内置角色的权限自 2026-09-15 起可改，故这个哨兵现在只剩「不可删」这一层含义。
	ErrBuiltinRole = errors.New("内置角色不可删除")
	// ErrRoleLocked 表示试图修改不对站长开放的角色（超级管理员）。
	ErrRoleLocked = errors.New("超级管理员角色不对外开放")
)

// Store 提供用户与角色的持久化操作。
type Store struct {
	db bun.IDB
}

// NewStore 构造 Store。
func NewStore(db bun.IDB) *Store {
	return &Store{db: db}
}

// SeedRoles 幂等地写入内置角色。
//
// 每次启动都执行，但只对两类字段负责：
//
//   - 显示名与描述**随代码覆盖** —— 措辞是产品的一部分，升级就该更新；
//   - 权限集合**只在角色首次创建时写入**。2026-09-15 起内置角色的权限归站长
//     （三挡角色都能在角色管理页自由勾选），每次启动覆盖回去等于无声改写站长的选择：
//     升级时新增一条权限本可以自动放宽，而删掉一条权限却可能让正在用的账号突然 403。
//
// super-admin 例外（perm.Locked）：它不对站长开放，权限每次都按代码覆盖。
// 代价是升级新增的权限不会自动落到已被改过的内置角色上，故角色管理页提供「恢复默认」。
func (s *Store) SeedRoles(ctx context.Context) error {
	for _, name := range perm.BuiltinRoleNames {
		meta := perm.BuiltinRoleMeta[name]
		role := &Role{
			Name:        name,
			Label:       meta.Label,
			Description: meta.Description,
			Permissions: perm.BuiltinRoles[name],
			Builtin:     true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		query := s.db.NewInsert().
			Model(role).
			On("CONFLICT (name) DO UPDATE").
			Set("builtin = true").
			Set("label = EXCLUDED.label").
			Set("description = EXCLUDED.description").
			Set("updated_at = now()")
		if perm.Locked(name) {
			query = query.Set("permissions = EXCLUDED.permissions")
		}
		if _, err := query.Exec(ctx); err != nil {
			return fmt.Errorf("写入内置角色 %s: %w", name, err)
		}
	}
	return nil
}

// CountUsers 返回用户总数，用于判断是否需要初始化首个管理员。
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	count, err := s.db.NewSelect().Model((*User)(nil)).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("统计用户数: %w", err)
	}
	return count, nil
}

// CreateUserParams 是创建用户的入参。
type CreateUserParams struct {
	Username    string
	Email       string
	Password    string
	DisplayName string
	Roles       []string
	// EmailVerified 为真时把 email_verified_at 一并写上。
	//
	// 后台建号（Console 用户管理、`lumo admin create-user`、Bootstrap）一律传 true：
	// 管理员当面给的账号再要求对方去收信毫无意义，而且站点没配 SMTP 时那封信根本发不出去。
	// 只有前台自助注册传 false —— 那条路径上的账号必须自己证明邮箱可达。
	EmailVerified bool
}

// CreateUser 创建用户并授予角色。
//
// 密码在此处哈希，调用方不应接触哈希逻辑。
func (s *Store) CreateUser(ctx context.Context, params *CreateUserParams) (*User, error) {
	username := NormalizeUsername(params.Username)
	email := strings.TrimSpace(strings.ToLower(params.Email))

	if err := password.Validate(params.Password); err != nil {
		return nil, err
	}
	hash, err := password.Hash(params.Password)
	if err != nil {
		return nil, err
	}

	var verifiedAt *time.Time
	if params.EmailVerified {
		now := time.Now()
		verifiedAt = &now
	}

	user := &User{
		Username:        username,
		Email:           email,
		PasswordHash:    hash,
		DisplayName:     strings.TrimSpace(params.DisplayName),
		EmailVerifiedAt: verifiedAt,
		Verified:        verifiedAt != nil,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	err = s.runInTx(ctx, func(tx bun.Tx) error {
		if _, insErr := tx.NewInsert().Model(user).Exec(ctx); insErr != nil {
			if isUniqueViolation(insErr) {
				return fmt.Errorf("%w: 用户名或邮箱已被占用", ErrDuplicate)
			}
			return fmt.Errorf("插入用户: %w", insErr)
		}
		return assignRoles(ctx, tx, user.ID, params.Roles)
	})
	if err != nil {
		return nil, err
	}

	if err := s.LoadRoles(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// FindUserByID 按 ID 查用户。
func (s *Store) FindUserByID(ctx context.Context, id int64) (*User, error) {
	user := new(User)
	err := s.db.NewSelect().Model(user).Where("u.id = ?", id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询用户: %w", err)
	}
	if err := s.LoadRoles(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// FindUserByLogin 按用户名或邮箱查用户，两者均大小写不敏感。
func (s *Store) FindUserByLogin(ctx context.Context, login string) (*User, error) {
	needle := strings.TrimSpace(strings.ToLower(login))
	if needle == "" {
		return nil, ErrNotFound
	}

	user := new(User)
	err := s.db.NewSelect().Model(user).
		Where("lower(u.username) = ? OR lower(u.email) = ?", needle, needle).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询用户: %w", err)
	}
	if err := s.LoadRoles(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// LoadRoles 填充用户的角色列表。
func (s *Store) LoadRoles(ctx context.Context, user *User) error {
	var roles []Role
	err := s.db.NewSelect().
		Model(&roles).
		Join("JOIN user_roles AS ur ON ur.role_id = r.id").
		Where("ur.user_id = ?", user.ID).
		Order("r.name").
		Scan(ctx)
	if err != nil {
		return fmt.Errorf("查询用户角色: %w", err)
	}
	user.Roles = roles
	return nil
}

// UpdatePassword 重设用户密码。
func (s *Store) UpdatePassword(ctx context.Context, userID int64, plain string) error {
	if err := password.Validate(plain); err != nil {
		return err
	}
	hash, err := password.Hash(plain)
	if err != nil {
		return err
	}
	return s.setPasswordHash(ctx, userID, hash)
}

// setPasswordHash 直接写入哈希，供密码重设与登录时透明升级使用。
func (s *Store) setPasswordHash(ctx context.Context, userID int64, hash string) error {
	res, err := s.db.NewUpdate().
		Model((*User)(nil)).
		Set("password_hash = ?", hash).
		Set("updated_at = now()").
		Where("id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新密码: %w", err)
	}
	return requireOneRow(res, "用户")
}

// TouchLastLogin 记录最近登录时间。
func (s *Store) TouchLastLogin(ctx context.Context, userID int64) error {
	_, err := s.db.NewUpdate().
		Model((*User)(nil)).
		Set("last_login_at = now()").
		Where("id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新登录时间: %w", err)
	}
	return nil
}

// SetDisabled 启用或停用账号。
func (s *Store) SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	res, err := s.db.NewUpdate().
		Model((*User)(nil)).
		Set("disabled = ?", disabled).
		Set("updated_at = now()").
		Where("id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("更新账号状态: %w", err)
	}
	return requireOneRow(res, "用户")
}

// AssignRoles 覆盖式设置用户角色。
func (s *Store) AssignRoles(ctx context.Context, userID int64, roleNames []string) error {
	return s.runInTx(ctx, func(tx bun.Tx) error {
		if _, err := tx.NewDelete().
			Model((*UserRole)(nil)).
			Where("user_id = ?", userID).
			Exec(ctx); err != nil {
			return fmt.Errorf("清除原有角色: %w", err)
		}
		return assignRoles(ctx, tx, userID, roleNames)
	})
}

// ListRoles 返回全部角色。
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	err := s.db.NewSelect().Model(&roles).Order("r.name").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询角色列表: %w", err)
	}
	for i := range roles {
		roles[i].fillMeta()
	}
	return roles, nil
}

// FindRoleByName 按名称查角色。
func (s *Store) FindRoleByName(ctx context.Context, name string) (*Role, error) {
	role := new(Role)
	err := s.db.NewSelect().Model(role).Where("r.name = ?", name).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("查询角色: %w", err)
	}
	return role, nil
}

// CustomRolePermissions 返回自定义角色的权限映射，供权限汇总使用。
func (s *Store) CustomRolePermissions(ctx context.Context) (map[string][]perm.Permission, error) {
	var roles []Role
	err := s.db.NewSelect().Model(&roles).Where("builtin = false").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询自定义角色: %w", err)
	}
	out := make(map[string][]perm.Permission, len(roles))
	for i := range roles {
		out[roles[i].Name] = roles[i].Permissions
	}
	return out, nil
}

// assignRoles 按角色名授予角色，未知角色名报错而非静默忽略。
func assignRoles(ctx context.Context, tx bun.Tx, userID int64, roleNames []string) error {
	if len(roleNames) == 0 {
		return nil
	}

	var roles []Role
	if err := tx.NewSelect().
		Model(&roles).
		Where("r.name IN (?)", bun.List(roleNames)).
		Scan(ctx); err != nil {
		return fmt.Errorf("查询角色: %w", err)
	}

	found := make(map[string]bool, len(roles))
	links := make([]UserRole, 0, len(roles))
	for i := range roles {
		found[roles[i].Name] = true
		links = append(links, UserRole{UserID: userID, RoleID: roles[i].ID, GrantedAt: time.Now()})
	}
	for _, name := range roleNames {
		if !found[name] {
			return fmt.Errorf("%w: 角色 %q", ErrNotFound, name)
		}
	}

	if _, err := tx.NewInsert().
		Model(&links).
		On("CONFLICT (user_id, role_id) DO NOTHING").
		Exec(ctx); err != nil {
		return fmt.Errorf("授予角色: %w", err)
	}
	return nil
}

// runInTx 在事务中执行 fn；若当前 IDB 本身已是事务则直接复用。
func (s *Store) runInTx(ctx context.Context, fn func(tx bun.Tx) error) error {
	if tx, ok := s.db.(bun.Tx); ok {
		return fn(tx)
	}
	db, ok := s.db.(*bun.DB)
	if !ok {
		return errors.New("auth: 无法在当前连接上开启事务")
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
}

// NormalizeUsername 归一化用户名：去空白并转小写。
//
// 导出是给 account 模块用的：注册表单要先归一化再校验格式，
// 否则用户输入 "Alice" 会因为含大写而被自己的正则拒掉，
// 而落到库里其实是合法的 "alice"。
func NormalizeUsername(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

// isUniqueViolation 判断是否为唯一约束冲突（PostgreSQL SQLSTATE 23505）。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// 不依赖 pgconn 具体类型：bun 会包装驱动错误，字符串匹配在此处足够稳健，
	// 且避免为一个判断引入对驱动内部类型的依赖。
	msg := err.Error()
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "重复键违反唯一约束")
}

// requireOneRow 校验更新确实命中了一行。
func requireOneRow(res sql.Result, what string) error {
	affected, err := res.RowsAffected()
	if err != nil {
		// 驱动不支持计数时不能据此判失败。
		return nil //nolint:nilerr // RowsAffected 不被支持时视为成功
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}
	return nil
}
