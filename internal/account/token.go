package account

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/auth"
)

// 令牌用途。与 account_tokens 表的 account_tokens_purpose CHECK 一一对应：
// 拼错的用途在 Go 这边就会被拒绝，不必等数据库报约束错。
const (
	PurposeVerifyEmail   = "verify_email"
	PurposeResetPassword = "reset_password"
)

// ErrInvalidToken 表示令牌不存在、已用过或已过期。
//
// 三种情况合并成一个错误：对用户而言处理方式完全相同（重新走一遍流程），
// 而区分开只会顺带告诉试探者「这一枚是真的，只是用过了」。
var ErrInvalidToken = errors.New("链接无效或已过期")

// Token 是一次性令牌记录。
//
// 库中只存 SHA-256（与 sessions、access_tokens 同一策略）：明文只出现在那封邮件的链接里，
// 库泄漏不等于账号失守。
type Token struct {
	bun.BaseModel `bun:"table:account_tokens,alias:at"`

	TokenHash string     `bun:"token_hash,pk"`
	UserID    int64      `bun:"user_id,notnull"`
	Purpose   string     `bun:"purpose,notnull"`
	ExpiresAt time.Time  `bun:"expires_at,notnull"`
	UsedAt    *time.Time `bun:"used_at"`
	CreatedAt time.Time  `bun:"created_at,nullzero"`
}

// TokenStore 提供一次性令牌的签发、查验与消费。
type TokenStore struct {
	db bun.IDB
}

// NewTokenStore 构造 TokenStore。
func NewTokenStore(db bun.IDB) *TokenStore { return &TokenStore{db: db} }

// Issue 签发一枚令牌，返回明文（只在此刻可见）。
//
// 签发即作废同用户同用途的旧令牌：用户点了「重新发送验证邮件」之后，
// 上一封邮件里的链接就该失效——否则每点一次就多留一个仍能改掉密码的入口。
func (s *TokenStore) Issue(ctx context.Context, userID int64, purpose string, ttl time.Duration) (string, error) {
	if !validPurpose(purpose) {
		return "", fmt.Errorf("未知的令牌用途 %q", purpose)
	}
	plaintext, err := randomToken()
	if err != nil {
		return "", err
	}

	token := &Token{
		TokenHash: auth.HashToken(plaintext),
		UserID:    userID,
		Purpose:   purpose,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}

	err = s.runInTx(ctx, func(tx bun.Tx) error {
		if _, delErr := tx.NewDelete().
			Model((*Token)(nil)).
			Where("at.user_id = ? AND at.purpose = ?", userID, purpose).
			Exec(ctx); delErr != nil {
			return fmt.Errorf("作废旧令牌: %w", delErr)
		}
		_, insErr := tx.NewInsert().Model(token).Exec(ctx)
		if insErr != nil {
			return fmt.Errorf("写入令牌: %w", insErr)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return plaintext, nil
}

// Check 查验令牌有效但**不消费**它，返回所属用户 ID。
//
// 供「打开重置密码页」这一步使用：那时用户还没设新密码，消费掉令牌等于
// 让一次误刷新就把链接烧掉。
func (s *TokenStore) Check(ctx context.Context, plaintext, purpose string) (int64, error) {
	if plaintext == "" {
		return 0, ErrInvalidToken
	}
	token := new(Token)
	err := s.db.NewSelect().
		Model(token).
		Where("at.token_hash = ? AND at.purpose = ? AND at.used_at IS NULL AND at.expires_at > now()",
			auth.HashToken(plaintext), purpose).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidToken
		}
		return 0, fmt.Errorf("查验令牌: %w", err)
	}
	return token.UserID, nil
}

// Consume 原子地消费令牌，返回所属用户 ID。
//
// 必须是一条 UPDATE 按影响行数判成败：先 SELECT 再 UPDATE 的话，
// 同一封邮件被并发点两次会重置两次密码，第二次覆盖掉第一次刚设的新密码。
// 单条 UPDATE 由数据库的行锁保证只有一个调用者能把它从「未用」改成「已用」。
func (s *TokenStore) Consume(ctx context.Context, plaintext, purpose string) (int64, error) {
	if plaintext == "" {
		return 0, ErrInvalidToken
	}
	var userID int64
	err := s.db.NewRaw(`UPDATE account_tokens SET used_at = now()
		WHERE token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > now()
		RETURNING user_id`,
		auth.HashToken(plaintext), purpose).Scan(ctx, &userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInvalidToken
		}
		return 0, fmt.Errorf("消费令牌: %w", err)
	}
	return userID, nil
}

// DeleteExpired 删除过期超过 retention 的令牌，返回删除条数。
//
// 不删「刚过期」的：留一段时间才回答得了「链接为什么失效」这个问题
// （是过期了，还是早就被用过了）。截止时间在 Go 侧算好再传参，
// 避免把 interval 字面量拼进 SQL。
func (s *TokenStore) DeleteExpired(ctx context.Context, retention time.Duration) (int64, error) {
	res, err := s.db.NewDelete().
		Model((*Token)(nil)).
		Where("at.expires_at <= ?", time.Now().Add(-retention)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("清理过期令牌: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		// 驱动不支持计数时不视为失败：这只是机会性清理，下一次循环会再试。
		return 0, nil //nolint:nilerr // 见上
	}
	return affected, nil
}

// validPurpose 报告用途是否在允许列表内。
func validPurpose(purpose string) bool {
	return purpose == PurposeVerifyEmail || purpose == PurposeResetPassword
}

// runInTx 在事务中执行 fn；若当前 IDB 本身已是事务则直接复用。
//
// 与 auth.Store 的同名方法同形。这里不跨包复用是因为它只有六行，
// 而把它提到公共位置会让 account 反向依赖 auth 的内部实现细节。
func (s *TokenStore) runInTx(ctx context.Context, fn func(tx bun.Tx) error) error {
	if tx, ok := s.db.(bun.Tx); ok {
		return fn(tx)
	}
	db, ok := s.db.(*bun.DB)
	if !ok {
		return errors.New("account: 无法在当前连接上开启事务")
	}
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
}

// randomToken 生成 32 字节随机令牌并做 URL 安全编码。
//
// 256 位熵：没有字典攻击面，故哈希直接复用 auth.HashToken（SHA-256 十六进制），
// 不需要加盐或慢哈希——慢哈希是为了防口令被穷举，而这里没有可穷举的空间。
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机令牌: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
