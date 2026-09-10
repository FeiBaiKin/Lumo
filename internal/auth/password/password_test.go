package password

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	t.Parallel()

	const plain = "correct horse battery staple"
	encoded, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Errorf("哈希串应为 PHC 格式，实际 %q", encoded)
	}
	// 明文绝不能出现在哈希串中。
	if strings.Contains(encoded, plain) {
		t.Fatal("哈希串包含明文密码")
	}

	if err := Verify(plain, encoded); err != nil {
		t.Errorf("正确密码应校验通过: %v", err)
	}
	if err := Verify("wrong password", encoded); !errors.Is(err, ErrMismatch) {
		t.Errorf("错误密码应返回 ErrMismatch，实际 %v", err)
	}
}

// TestHashIsSalted 验证同一密码两次哈希结果不同（盐随机）。
func TestHashIsSalted(t *testing.T) {
	t.Parallel()

	const plain = "same-password-twice"
	first, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}
	second, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}

	if first == second {
		t.Fatal("同一密码的两次哈希不应相同，说明盐未随机化")
	}
	// 两者都必须能校验通过。
	if err := Verify(plain, first); err != nil {
		t.Errorf("第一个哈希校验失败: %v", err)
	}
	if err := Verify(plain, second); err != nil {
		t.Errorf("第二个哈希校验失败: %v", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		plain   string
		wantErr bool
	}{
		{"合法密码", "abcdefgh", false},
		{"空密码", "", true},
		{"过短", "abc", true},
		{"刚好达到下限", strings.Repeat("a", MinLength), false},
		{"超过上限", strings.Repeat("a", MaxLength+1), true},
		{"中文密码", "这是一个很长的中文密码", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tt.plain)
			if tt.wantErr && err == nil {
				t.Error("期望校验失败")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("期望校验通过，实际 %v", err)
			}
		})
	}
}

// TestHashRejectsOverlongPassword 验证超长密码被拒绝——
// 不限长会让超长输入成为廉价的 DoS 手段。
func TestHashRejectsOverlongPassword(t *testing.T) {
	t.Parallel()

	if _, err := Hash(strings.Repeat("x", MaxLength+1)); err == nil {
		t.Fatal("超长密码应被拒绝")
	}
	if _, err := Hash(""); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("空密码应返回 ErrEmptyPassword，实际 %v", err)
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",
		"not-a-hash",
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",      // 算法不对
		"$argon2id$v=18$m=65536,t=3,p=4$c2FsdA$aGFzaA",     // 版本不兼容
		"$argon2id$v=19$m=0,t=3,p=4$c2FsdA$aGFzaA",         // 参数为 0
		"$argon2id$v=19$m=65536,t=3,p=4$!!!invalid$aGFzaA", // 盐非法 base64
		"$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$!!!invalid", // 哈希非法 base64
		"$argon2id$v=19$m=65536,t=3,p=4$c2FsdA",            // 段数不足
	}

	for _, encoded := range bad {
		if _, _, _, err := Decode(encoded); !errors.Is(err, ErrInvalidHash) {
			t.Errorf("Decode(%q) 应返回 ErrInvalidHash，实际 %v", encoded, err)
		}
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	t.Parallel()

	if err := Verify("any", "garbage"); !errors.Is(err, ErrInvalidHash) {
		t.Errorf("非法哈希应返回 ErrInvalidHash，实际 %v", err)
	}
}

// TestDecodeRoundTrip 验证参数能从哈希串中原样取回，
// 这是「调整默认参数不使旧哈希失效」的前提。
func TestDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	want := Params{Memory: 32 * 1024, Iterations: 2, Parallelism: 2, SaltLength: 16, KeyLength: 32}
	encoded, err := HashWithParams("password123", want)
	if err != nil {
		t.Fatalf("HashWithParams 失败: %v", err)
	}

	got, salt, key, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode 失败: %v", err)
	}
	if got.Memory != want.Memory || got.Iterations != want.Iterations || got.Parallelism != want.Parallelism {
		t.Errorf("参数未原样取回: %+v，期望 %+v", got, want)
	}
	if len(salt) != int(want.SaltLength) {
		t.Errorf("盐长度 = %d，期望 %d", len(salt), want.SaltLength)
	}
	if len(key) != int(want.KeyLength) {
		t.Errorf("哈希长度 = %d，期望 %d", len(key), want.KeyLength)
	}

	// 用弱参数生成的哈希仍应能校验通过。
	if err := Verify("password123", encoded); err != nil {
		t.Errorf("弱参数哈希应仍可校验: %v", err)
	}
}

func TestNeedsRehash(t *testing.T) {
	t.Parallel()

	weak, err := HashWithParams("password123", Params{
		Memory: 16 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	if err != nil {
		t.Fatalf("HashWithParams 失败: %v", err)
	}
	if !NeedsRehash(weak) {
		t.Error("弱参数哈希应需要重算")
	}

	current, err := Hash("password123")
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}
	if NeedsRehash(current) {
		t.Error("当前参数哈希不应需要重算")
	}

	// 无法解析的哈希理应重算。
	if !NeedsRehash("garbage") {
		t.Error("非法哈希应需要重算")
	}
}

func TestDefaultParamsAreSane(t *testing.T) {
	t.Parallel()

	p := DefaultParams()
	// OWASP 对 argon2id 的内存下限建议为 19 MiB。
	if p.Memory < 19*1024 {
		t.Errorf("内存开销 %d KiB 低于建议下限", p.Memory)
	}
	if p.Iterations < 2 {
		t.Errorf("迭代次数 %d 过低", p.Iterations)
	}
	if p.Parallelism < 1 || p.Parallelism > 4 {
		t.Errorf("并行度 %d 超出预期范围 [1,4]", p.Parallelism)
	}
	if p.KeyLength < 32 || p.SaltLength < 16 {
		t.Errorf("盐或哈希长度过短: %+v", p)
	}
}
