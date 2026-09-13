package account

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/database"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// newTokenEnv 建一套只含核心 users 表与 account_tokens 表的最小环境。
//
// 不整个模块装配：本文件验的是令牌的签发 / 查验 / 消费 / 清理这四件事的原子性，
// 把主题、设置、邮件都拉进来只会让失败信息更难读。
func newTokenEnv(t *testing.T) (tokens *TokenStore, store *Store, users *auth.Store, db *database.DB) {
	t.Helper()

	db = testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{{Name: Name, FS: New().Migrations()}},
	})
	users = auth.NewStore(db.DB)
	if err := users.SeedRoles(context.Background()); err != nil {
		t.Fatalf("写入内置角色失败: %v", err)
	}
	return NewTokenStore(db.DB), NewStore(db.DB), users, db
}

// createUser 建一个已验证的测试用户。
func createUser(t *testing.T, users *auth.Store, username string) *auth.User {
	t.Helper()
	user, err := users.CreateUser(context.Background(), &auth.CreateUserParams{
		Username: username, Email: username + "@example.com",
		Password: testPassword, Roles: []string{perm.RoleMember}, EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return user
}

// TestTokenIssueCheckConsume 验证一次完整的签发 → 查验 → 消费。
//
// 查验不消费是关键：重置密码页要在用户**还没**设新密码时校验链接，
// 那一步若把令牌烧掉，一次误刷新就废掉了用户手里唯一的那枚链接。
func TestTokenIssueCheckConsume(t *testing.T) {
	tokens, _, users, _ := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "tok")

	plaintext, err := tokens.Issue(ctx, user.ID, PurposeResetPassword, ResetPasswordTTL)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if plaintext == "" {
		t.Fatal("签发应返回明文")
	}

	for i := 0; i < 2; i++ {
		got, checkErr := tokens.Check(ctx, plaintext, PurposeResetPassword)
		if checkErr != nil {
			t.Fatalf("第 %d 次查验失败: %v", i+1, checkErr)
		}
		if got != user.ID {
			t.Errorf("查验返回的用户 ID = %d，期望 %d", got, user.ID)
		}
	}

	got, err := tokens.Consume(ctx, plaintext, PurposeResetPassword)
	if err != nil {
		t.Fatalf("消费失败: %v", err)
	}
	if got != user.ID {
		t.Errorf("消费返回的用户 ID = %d，期望 %d", got, user.ID)
	}
}

// TestTokenConsumeIsAtomic 验证同一枚令牌只能被消费一次。
//
// 这条守的是并发场景：同一封邮件被点两次（或用户开了两个标签页同时提交），
// 第二次必须失败。先 SELECT 再 UPDATE 的实现会两次都通过，
// 于是第二次重置覆盖掉第一次刚设的新密码——用户以为自己设成了 A，实际是 B。
func TestTokenConsumeIsAtomic(t *testing.T) {
	tokens, _, users, _ := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "atomic")

	plaintext, err := tokens.Issue(ctx, user.ID, PurposeResetPassword, ResetPasswordTTL)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := tokens.Consume(ctx, plaintext, PurposeResetPassword); err != nil {
		t.Fatalf("首次消费应成功: %v", err)
	}
	if _, err := tokens.Consume(ctx, plaintext, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("第二次消费应返回 ErrInvalidToken，实际 %v", err)
	}
	// 消费之后连查验也必须失败。
	if _, err := tokens.Check(ctx, plaintext, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("已消费的令牌应查验失败，实际 %v", err)
	}
}

// TestTokenIssueInvalidatesPrevious 验证签发新令牌会作废旧令牌。
//
// 用户点「重新发送」之后，上一封邮件里的链接就该失效。
// 不作废的话，每点一次就多留一个仍能改掉密码的入口。
func TestTokenIssueInvalidatesPrevious(t *testing.T) {
	tokens, _, users, _ := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "reissue")

	old, err := tokens.Issue(ctx, user.ID, PurposeResetPassword, ResetPasswordTTL)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := tokens.Issue(ctx, user.ID, PurposeResetPassword, ResetPasswordTTL); err != nil {
		t.Fatalf("重签失败: %v", err)
	}

	if _, err := tokens.Check(ctx, old, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("旧令牌应已失效，实际 %v", err)
	}
}

// TestTokenPurposesAreSeparate 验证两种用途互不通用。
//
// 一封验证邮件里的令牌不该能改密码：验证链接的有效期是 48 小时，
// 而重置链接只有 2 小时，正是因为后者的破坏力大得多。
func TestTokenPurposesAreSeparate(t *testing.T) {
	tokens, _, users, _ := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "purpose")

	verify, err := tokens.Issue(ctx, user.ID, PurposeVerifyEmail, VerifyEmailTTL)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := tokens.Check(ctx, verify, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("验证令牌不该能当重置令牌用，实际 %v", err)
	}
	if _, err := tokens.Issue(ctx, user.ID, "change_role", time.Hour); err == nil {
		t.Error("未知用途应被拒绝，而不是落库后撞 CHECK")
	}
}

// TestTokenRejectsExpired 验证过期令牌不可用。
func TestTokenRejectsExpired(t *testing.T) {
	tokens, _, users, _ := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "expired")

	// 负 TTL 直接签出一枚已经过期的令牌，不必等两小时。
	plaintext, err := tokens.Issue(ctx, user.ID, PurposeResetPassword, -time.Minute)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := tokens.Check(ctx, plaintext, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("过期令牌应查验失败，实际 %v", err)
	}
	if _, err := tokens.Consume(ctx, plaintext, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("过期令牌应消费失败，实际 %v", err)
	}
}

// TestTokenRejectsEmpty 验证空令牌直接拒绝，不打数据库。
func TestTokenRejectsEmpty(t *testing.T) {
	tokens, _, _, _ := newTokenEnv(t)
	ctx := context.Background()

	if _, err := tokens.Check(ctx, "", PurposeVerifyEmail); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("空令牌应被拒绝，实际 %v", err)
	}
	if _, err := tokens.Consume(ctx, "", PurposeVerifyEmail); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("空令牌应被拒绝，实际 %v", err)
	}
}

// TestStoreMarkEmailVerifiedIsIdempotent 验证重复标记不会改写第一次的时间。
//
// 用户可能开了两个标签页各点一次验证链接；第二次把时间改成更晚的值，
// 会让「这个邮箱是什么时候验证的」这个审计问题失去答案。
func TestStoreMarkEmailVerifiedIsIdempotent(t *testing.T) {
	_, store, users, db := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "verify")

	// 先把它改回未验证，模拟自助注册出来的账号。
	if _, err := db.ExecContext(ctx, "UPDATE users SET email_verified_at = NULL WHERE id = ?", user.ID); err != nil {
		t.Fatalf("重置验证状态失败: %v", err)
	}

	if err := store.MarkEmailVerified(ctx, user.ID); err != nil {
		t.Fatalf("标记失败: %v", err)
	}
	first, err := store.FindByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !first.Verified() {
		t.Fatal("标记后应为已验证")
	}

	if markErr := store.MarkEmailVerified(ctx, user.ID); markErr != nil {
		t.Fatalf("重复标记失败: %v", markErr)
	}
	second, err := store.FindByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !second.EmailVerifiedAt.Equal(*first.EmailVerifiedAt) {
		t.Errorf("重复标记不该改写验证时间：%v → %v", first.EmailVerifiedAt, second.EmailVerifiedAt)
	}
}

// TestTokenDeleteExpiredKeepsRecent 验证清理只删过期已久的行。
//
// 保留期是有用的：排查「链接为什么失效」时，一行还在表里就能回答
// 「是过期了，还是早就被用过了」。删得太早，这个问题就只剩猜。
func TestTokenDeleteExpiredKeepsRecent(t *testing.T) {
	tokens, _, users, _ := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "sweep")

	// 一枚刚刚过期（1 分钟前），一枚过期很久（30 天前）。
	if _, err := tokens.Issue(ctx, user.ID, PurposeVerifyEmail, -time.Minute); err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	old, err := tokens.Issue(ctx, user.ID, PurposeResetPassword, -30*24*time.Hour)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}

	removed, err := tokens.DeleteExpired(ctx, tokenRetention)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if removed != 1 {
		t.Errorf("应只删掉过期超过保留期的那一枚，实际删了 %d", removed)
	}
	if _, err := tokens.Check(ctx, old, PurposeResetPassword); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("被清理的令牌应查不到，实际 %v", err)
	}
}

// TestStoreFindVerifiedByEmailSkipsUnverified 验证找回密码只认已验证的邮箱。
func TestStoreFindVerifiedByEmailSkipsUnverified(t *testing.T) {
	_, store, users, db := newTokenEnv(t)
	ctx := context.Background()
	user := createUser(t, users, "unverifiedmail")

	if _, err := db.ExecContext(ctx, "UPDATE users SET email_verified_at = NULL WHERE id = ?", user.ID); err != nil {
		t.Fatalf("重置验证状态失败: %v", err)
	}

	if _, err := store.FindVerifiedByEmail(ctx, user.Email); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("未验证的邮箱不该被找回密码流程认到，实际 %v", err)
	}
	if _, err := store.FindByEmail(ctx, user.Email); err != nil {
		t.Errorf("FindByEmail 应能查到未验证的账号: %v", err)
	}
}
