package main

import (
	"log/slog"

	"github.com/FeiBaiKin/lumo/internal/api"
	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/database"
)

// coreStack 是核心自带的认证与管理能力。
//
// 抽成一处是为了让 serve 与 openapi 注册**同一批**端点：导出的规范一旦少了登录接口，
// Console 生成出来的客户端就没有登录方法，而这种偏差要到联调时才会发现。
type coreStack struct {
	Users         *auth.Store
	Sessions      *auth.SessionStore
	Tokens        *auth.TokenStore
	Service       *auth.Service
	Authenticator *auth.Authenticator
}

// newCoreStack 构造认证栈。只做装配、不访问数据库，可在迁移之前调用。
func newCoreStack(db *database.DB, secureCookies bool, logger *slog.Logger) *coreStack {
	users := auth.NewStore(db.DB)
	sessions := auth.NewSessionStore(db.DB, secureCookies)
	tokens := auth.NewTokenStore(db.DB)
	return &coreStack{
		Users:         users,
		Sessions:      sessions,
		Tokens:        tokens,
		Service:       auth.NewService(users, sessions, tokens, logger),
		Authenticator: auth.NewAuthenticator(users, sessions, tokens, logger),
	}
}

// registerAPI 挂上核心端点，再装配全部功能模块。
//
// 顺序有讲究：核心的处理器要先于模块构造，而权限清单必须延迟到请求时才读——
// 声明权限的模块要到本函数最后一行才注册。
func (c *coreStack) registerAPI(planes *api.Planes, application *app.App, logger *slog.Logger) error {
	// 认证端点：登录走免认证注册面，其余走强制认证注册面。
	auth.NewHandler(c.Service, c.Sessions, c.Tokens, logger).
		Register(planes.ConsolePublic(), planes.Console())
	auth.NewAdminHandler(c.Users, c.Service, func() []auth.PermissionInfo {
		// 核心自身引入的权限与各模块声明的合并：前者没有对应的功能模块。
		declared := append(app.CorePermissions(), application.Permissions()...)
		out := make([]auth.PermissionInfo, 0, len(declared))
		for _, p := range declared {
			out = append(out, auth.PermissionInfo{Key: p.Key, Label: p.Label, Description: p.Description})
		}
		return out
	}).Register(planes.Console())

	return application.Register(modules()...)
}
