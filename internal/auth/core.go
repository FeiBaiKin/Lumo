package auth

import (
	"log/slog"

	"github.com/uptrace/bun"
)

// CoreKey 是核心认证栈在 app 容器里的登记名。
//
// 放在 auth 包里而不是使用方：登记名与类型是一件事，分开放迟早会出现
// 「Provide 用了这个名字、Lookup 拼成了另一个」这种只在运行时暴露的错。
const CoreKey = "auth-core"

// Core 是核心自带的认证栈。
//
// 抽成一个类型是为了让功能模块能经 app.Lookup 取到**同一批实例**，而不是各自 new 一套：
// LoginLimiter 是进程内存计数，模块各建一份等于把限流额度翻倍——前台登录
// 就成了绕过后台登录限流的旁路。同理，会话存储的 Secure 开关、
// argon2 的并发额度、改密踢下线的删除范围，都必须是同一份。
//
// 注意本包**不得 import internal/app**：app 已被 theme 等模块依赖，
// 反向引用会绕成导入环。因此取值函数（From）写在消费方，不写在这里。
type Core struct {
	Users         *Store
	Sessions      *SessionStore
	Tokens        *TokenStore
	Service       *Service
	Authenticator *Authenticator
}

// NewCore 构造认证栈。只做装配、不访问数据库，可在迁移之前调用。
func NewCore(db bun.IDB, secureCookies bool, logger *slog.Logger) *Core {
	users := NewStore(db)
	sessions := NewSessionStore(db, secureCookies)
	tokens := NewTokenStore(db)
	return &Core{
		Users:         users,
		Sessions:      sessions,
		Tokens:        tokens,
		Service:       NewService(users, sessions, tokens, logger),
		Authenticator: NewAuthenticator(users, sessions, tokens, logger),
	}
}
