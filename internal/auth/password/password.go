// Package password 提供 argon2id 密码哈希与校验。
//
// 哈希串采用 PHC 标准格式，自带算法参数：
//
//	$argon2id$v=19$m=65536,t=3,p=4$<base64 salt>$<base64 hash>
//
// 参数随哈希一同存储，因此调整默认参数不会使已有哈希失效——
// 旧密码仍可校验，并可在用户下次登录时按需重算（见 NeedsRehash）。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params 是 argon2id 的开销参数。
type Params struct {
	// Memory 是内存开销，单位 KiB。
	Memory uint32
	// Iterations 是时间开销（迭代次数）。
	Iterations uint32
	// Parallelism 是并行度（线程数）。
	Parallelism uint8
	// SaltLength 是盐的字节数。
	SaltLength uint32
	// KeyLength 是输出哈希的字节数。
	KeyLength uint32
}

// DefaultParams 返回默认开销参数。
//
// 取值参考 OWASP 对 argon2id 的建议（19 MiB / t=2 为下限），此处取更保守的
// 64 MiB / t=3。并行度跟随 CPU 核数但封顶 4：过高并不增加安全性，
// 却会在并发登录时放大内存占用。
func DefaultParams() Params {
	parallelism := runtime.NumCPU()
	if parallelism > 4 {
		parallelism = 4
	}
	if parallelism < 1 {
		parallelism = 1
	}
	return Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: uint8(parallelism), //nolint:gosec // 已在上方限制到 [1,4]
		SaltLength:  16,
		KeyLength:   32,
	}
}

// 错误哨兵。
var (
	// ErrMismatch 表示密码与哈希不匹配。
	ErrMismatch = errors.New("密码不匹配")
	// ErrInvalidHash 表示哈希串格式非法。
	ErrInvalidHash = errors.New("哈希串格式非法")
	// ErrEmptyPassword 表示密码为空。
	ErrEmptyPassword = errors.New("密码不能为空")
	// ErrBusy 表示哈希并发额度已满，稍后重试即可。
	ErrBusy = errors.New("密码校验繁忙，请稍后重试")
)

// 进程级哈希并发闸门。
//
// argon2id 按当前参数一次要 64 MiB，而匿名登录请求**即使账号不存在**也会
// 走一次等价开销的校验（抹平时间差）。没有闸门时，几百个并发登录请求
// 就是几百份 64 MiB —— 小请求即可把内存打满。
//
// 并发度按 CPU 核数取，并封顶 8：再多也不会更快，只是排队更长。
// 真正的防护是 auth 包的登录限流，这里兜住的是「限流被绕过或尚未触发」时的峰值。
var hashSlots = func() chan struct{} {
	limit := runtime.NumCPU()
	if limit > 8 {
		limit = 8
	}
	if limit < 1 {
		limit = 1
	}
	return make(chan struct{}, limit)
}()

// HashSlots 返回进程级并发哈希额度的总量，供运维观测与容量估算。
func HashSlots() int { return cap(hashSlots) }

// acquire 申请一个哈希额度；额度用尽时返回 ErrBusy 而不是无限等待。
//
// Wait 有上限：宁可让极少数请求快速失败并返回 503/429，
// 也不让它们堆在队列里占着连接与内存，把整站拖垮。
func acquire() error {
	select {
	case hashSlots <- struct{}{}:
		return nil
	default:
		return ErrBusy
	}
}

// release 归还哈希额度。
func release() { <-hashSlots }

// MinLength 是密码最小长度。
//
// 取 8 位与 OWASP 建议一致；上限见 MaxLength。
const MinLength = 8

// MaxLength 是密码最大长度。
//
// 必须设上限：argon2 的计算量与输入长度相关，不限长会让超长密码
// 成为廉价的 DoS 手段。72 字节以上的熵对本场景已无实际意义。
const MaxLength = 128

// Validate 校验密码是否满足长度要求。
func Validate(plain string) error {
	if plain == "" {
		return ErrEmptyPassword
	}
	if len(plain) < MinLength {
		return fmt.Errorf("密码长度不足 %d 位", MinLength)
	}
	if len(plain) > MaxLength {
		return fmt.Errorf("密码长度超过 %d 字节", MaxLength)
	}
	return nil
}

// Hash 用默认参数生成 argon2id 哈希串。
func Hash(plain string) (string, error) {
	return HashWithParams(plain, DefaultParams())
}

// HashWithParams 用指定参数生成 argon2id 哈希串。
func HashWithParams(plain string, p Params) (string, error) {
	if plain == "" {
		return "", ErrEmptyPassword
	}
	if len(plain) > MaxLength {
		return "", fmt.Errorf("密码长度超过 %d 字节", MaxLength)
	}

	if err := acquire(); err != nil {
		return "", err
	}
	defer release()

	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐失败: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify 校验明文密码与哈希串是否匹配。
//
// 返回 ErrMismatch 表示密码错误，ErrInvalidHash 表示哈希串本身有问题。
// 调用方应对两者给出**相同**的对外提示，避免泄漏账号是否存在。
func Verify(plain, encoded string) error {
	p, salt, want, err := Decode(encoded)
	if err != nil {
		return err
	}

	if err := acquire(); err != nil {
		return err
	}
	defer release()

	got := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	// 定长比较，避免通过响应时间推断哈希前缀。
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// Decode 解析 PHC 格式的 argon2id 哈希串。
func Decode(encoded string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// 形如 ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[0] != "" {
		return p, nil, nil, ErrInvalidHash
	}
	if parts[1] != "argon2id" {
		return p, nil, nil, fmt.Errorf("%w: 不支持的算法 %q", ErrInvalidHash, parts[1])
	}

	var version int
	if _, scanErr := fmt.Sscanf(parts[2], "v=%d", &version); scanErr != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return p, nil, nil, fmt.Errorf("%w: 不兼容的版本 %d", ErrInvalidHash, version)
	}

	if _, scanErr := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&p.Memory, &p.Iterations, &p.Parallelism); scanErr != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if p.Memory == 0 || p.Iterations == 0 || p.Parallelism == 0 {
		return p, nil, nil, fmt.Errorf("%w: 参数不能为 0", ErrInvalidHash)
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return p, nil, nil, ErrInvalidHash
	}

	p.SaltLength = uint32(len(salt)) //nolint:gosec // 盐长度远小于 uint32 上限
	p.KeyLength = uint32(len(key))   //nolint:gosec // 哈希长度远小于 uint32 上限
	return p, salt, key, nil
}

// NeedsRehash 报告已有哈希是否弱于当前默认参数。
//
// 用于在用户成功登录时透明升级哈希强度：此时明文密码在手，
// 是唯一能重算哈希的时机。
func NeedsRehash(encoded string) bool {
	p, _, _, err := Decode(encoded)
	if err != nil {
		// 无法解析的哈希理应重算。
		return true
	}
	want := DefaultParams()
	return p.Memory < want.Memory ||
		p.Iterations < want.Iterations ||
		p.KeyLength < want.KeyLength
}
