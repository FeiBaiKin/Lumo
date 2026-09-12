package auth_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
)

// TestLoginRateLimitEndToEnd 是审查发现的缺口的回归测试：
// 匿名登录请求即使账号不存在也要跑一次约 64 MiB 的 argon2 校验，
// 而登录接口没有任何限流，小请求即可显著放大 CPU 与内存消耗，且没有密码猜测防护。
//
// 这里把阈值压到 2，用最短的链路验证「失败计数 → 429 → 正确密码同样被挡」。
func TestLoginRateLimitEndToEnd(t *testing.T) {
	e := newEnv(t)
	// 阈值压小，避免用例里写死生产默认值（默认值由 ratelimit_test.go 覆盖）。
	e.service.SetLoginLimiter(auth.NewLoginLimiterWith(2, 100, time.Minute))
	root := newRouter(t, e)
	e.createUser(t, "erin", perm.RoleAuthor)

	fail := func(login, pass string) int {
		rec := e.do(t, root, &call{method: http.MethodPost, path: "/auth/login",
			body: `{"login":"` + login + `","password":"` + pass + `"}`})
		return rec.Code
	}

	// 前两次失败照常返回 401。
	for i := range 2 {
		if code := fail("erin", "wrong-password"); code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次失败登录状态码 = %d，期望 401", i+1, code)
		}
	}

	t.Run("额度用尽后返回 429", func(t *testing.T) {
		if code := fail("erin", "wrong-password"); code != http.StatusTooManyRequests {
			t.Fatalf("超限后状态码 = %d，期望 429", code)
		}
		// 正确密码同样被挡：限流只看尝试频率，不看这一次是否正确。
		if code := fail("erin", testPassword); code != http.StatusTooManyRequests {
			t.Errorf("超限后即使密码正确也应 429，实际 %d", code)
		}
		// 大小写不同的写法共享同一份额度。
		if code := fail("ERIN", testPassword); code != http.StatusTooManyRequests {
			t.Errorf("登录名大小写变体不应绕过限流，实际 %d", code)
		}
	})

	t.Run("不存在的账号同样被限流", func(t *testing.T) {
		// 账号不存在也要跑一次等价开销的哈希，因此必须是限流的第一等公民。
		e.service.SetLoginLimiter(auth.NewLoginLimiterWith(2, 100, time.Minute))
		fail("nobody", "wrong-password")
		fail("nobody", "wrong-password")
		if code := fail("nobody", "wrong-password"); code != http.StatusTooManyRequests {
			t.Errorf("枚举不存在的账号应被限流，实际 %d", code)
		}
	})

	t.Run("其他账号不受牵连", func(t *testing.T) {
		e.service.SetLoginLimiter(auth.NewLoginLimiterWith(2, 100, time.Minute))
		e.createUser(t, "frank", perm.RoleEditor)
		cookies, _ := e.login(t, root, "frank", testPassword)
		if len(cookies) == 0 {
			t.Error("另一个账号应能正常登录")
		}
	})

	t.Run("成功登录后本账号恢复额度", func(t *testing.T) {
		e.service.SetLoginLimiter(auth.NewLoginLimiterWith(2, 100, time.Minute))
		fail("frank", "wrong-password")
		// 输对一次即清空计数：不这么做，手滑几次的用户会在改对密码后继续被挡。
		if cookies, _ := e.login(t, root, "frank", testPassword); len(cookies) == 0 {
			t.Fatal("密码正确时应能登录")
		}
		fail("frank", "wrong-password")
		if code := fail("frank", "wrong-password"); code == http.StatusTooManyRequests {
			t.Error("成功登录应清空该账号的失败计数")
		}
	})
}
