package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/logging"
)

const adminUsage = `管理命令。

用法：
  lumo admin <子命令> [参数]

子命令：
  create-user      创建用户
  reset-password   重置用户密码（会使该用户的全部会话与令牌失效）
  list-users       列出用户

示例：
  lumo admin create-user -username alice -email alice@example.com -role super-admin
  lumo admin reset-password -user alice
`

// runAdmin 分派管理子命令。
func runAdmin(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, adminUsage)
		return errors.New("缺少子命令")
	}

	switch args[0] {
	case "create-user":
		return runCreateUser(args[1:])
	case "reset-password":
		return runResetPassword(args[1:])
	case "list-users":
		return runListUsers(args[1:])
	case "-h", "--help", "help":
		fmt.Print(adminUsage)
		return nil
	default:
		fmt.Fprint(os.Stderr, adminUsage)
		return fmt.Errorf("未知子命令 %q", args[0])
	}
}

// adminDeps 是管理命令共用的依赖。
type adminDeps struct {
	users    *auth.Store
	service  *auth.Service
	db       *database.DB
	teardown func()
}

// openAdminDeps 建立数据库连接并装配认证组件。
func openAdminDeps(ctx context.Context, configPath string) (*adminDeps, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if dsnErr := cfg.RequireDSN(); dsnErr != nil {
		return nil, dsnErr
	}

	logger := logging.New(os.Stdout, logging.Options{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})

	db, err := database.Open(ctx, cfg.Database, false)
	if err != nil {
		return nil, err
	}

	// 管理命令直接改动用户与凭据，先确认连到了哪个库（agent.md §13.2）。
	db.LogInfo(ctx, logger)

	users := auth.NewStore(db.DB)
	if err := users.SeedRoles(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	sessions := auth.NewSessionStore(db.DB, cfg.Server.SecureCookies)
	tokens := auth.NewTokenStore(db.DB)

	return &adminDeps{
		users:    users,
		service:  auth.NewService(users, sessions, tokens, logger),
		db:       db,
		teardown: func() { _ = db.Close() },
	}, nil
}

func runCreateUser(args []string) error {
	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	username := fs.String("username", "", "用户名（小写字母、数字、连字符）")
	email := fs.String("email", "", "邮箱")
	displayName := fs.String("display-name", "", "显示名，缺省时使用用户名")
	roles := fs.String("role", perm.RoleAdmin, "角色，多个用逗号分隔")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" || *email == "" {
		return errors.New("-username 与 -email 均为必填")
	}

	ctx := context.Background()
	deps, err := openAdminDeps(ctx, *configPath)
	if err != nil {
		return err
	}
	defer deps.teardown()

	plain, err := readNewPassword()
	if err != nil {
		return err
	}

	user, err := deps.users.CreateUser(ctx, &auth.CreateUserParams{
		Username:    *username,
		Email:       *email,
		Password:    plain,
		DisplayName: *displayName,
		Roles:       splitAndTrim(*roles),
		// CLI 建号视为已验证：管理员在终端里亲手给的账号，再去收一封验证信没有意义。
		EmailVerified: true,
	})
	if err != nil {
		return err
	}

	fmt.Printf("已创建用户 %s（ID %d），角色：%s\n",
		user.Username, user.ID, strings.Join(user.RoleNames(), ", "))
	return nil
}

func runResetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	login := fs.String("user", "", "用户名或邮箱")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *login == "" {
		return errors.New("-user 为必填")
	}

	ctx := context.Background()
	deps, err := openAdminDeps(ctx, *configPath)
	if err != nil {
		return err
	}
	defer deps.teardown()

	user, err := deps.users.FindUserByLogin(ctx, *login)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			return fmt.Errorf("用户 %q 不存在", *login)
		}
		return err
	}

	plain, err := readNewPassword()
	if err != nil {
		return err
	}

	if err := deps.service.ResetPassword(ctx, user.ID, plain); err != nil {
		return err
	}

	fmt.Printf("已重置用户 %s 的密码；其全部会话与访问令牌已失效\n", user.Username)
	return nil
}

func runListUsers(args []string) error {
	fs := flag.NewFlagSet("list-users", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	deps, err := openAdminDeps(ctx, *configPath)
	if err != nil {
		return err
	}
	defer deps.teardown()

	var users []auth.User
	if err := deps.db.NewSelect().Model(&users).Order("u.id").Scan(ctx); err != nil {
		return fmt.Errorf("查询用户列表: %w", err)
	}
	if len(users) == 0 {
		fmt.Println("暂无用户。可执行 lumo admin create-user 创建。")
		return nil
	}

	for i := range users {
		user := &users[i]
		if err := deps.users.LoadRoles(ctx, user); err != nil {
			return err
		}
		status := "启用"
		if user.Disabled {
			status = "停用"
		}
		fmt.Printf("%-5d %-20s %-30s %-8s %s\n",
			user.ID, user.Username, user.Email, status, strings.Join(user.RoleNames(), ","))
	}
	return nil
}

// readNewPassword 从终端读取新密码并要求确认。
//
// 用 term.ReadPassword 关闭回显，避免密码出现在屏幕与 shell 历史中；
// 也因此不提供 -password 参数：命令行参数会进入进程列表与历史记录。
func readNewPassword() (string, error) {
	stdin := int(os.Stdin.Fd())
	if !term.IsTerminal(stdin) {
		return "", errors.New("需要在交互式终端中运行以安全输入密码")
	}

	fmt.Print("请输入新密码：")
	first, err := term.ReadPassword(stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("读取密码: %w", err)
	}

	fmt.Print("请再次输入以确认：")
	second, err := term.ReadPassword(stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("读取密码: %w", err)
	}

	// 定长比较不必要（本地终端输入），但用 bytes.Equal 更直接。
	if !bytes.Equal(first, second) {
		return "", errors.New("两次输入的密码不一致")
	}
	return string(first), nil
}

// splitAndTrim 按逗号切分并去除空白项。
func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
