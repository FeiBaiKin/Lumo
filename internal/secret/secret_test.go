package secret

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKeyring(t *testing.T) *Keyring {
	t.Helper()
	key := make([]byte, keySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	k, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return k
}

func TestSealRoundTrip(t *testing.T) {
	k := newKeyring(t)

	for _, plain := range []string{"hunter2", "带中文的口令", strings.Repeat("x", 4096)} {
		sealed, err := k.Seal(plain)
		if err != nil {
			t.Fatalf("Seal(%q): %v", plain, err)
		}
		if !Encrypted(sealed) {
			t.Errorf("密文 %q 没有 enc: 前缀，读取时会当成明文", sealed)
		}
		if strings.Contains(sealed, plain) {
			t.Errorf("密文里能直接看到明文 %q", plain)
		}
		got, err := k.Unseal(sealed)
		if err != nil {
			t.Fatalf("Unseal: %v", err)
		}
		if got != plain {
			t.Errorf("还原得到 %q，期望 %q", got, plain)
		}
	}
}

func TestSealEmptyStaysEmpty(t *testing.T) {
	k := newKeyring(t)
	sealed, err := k.Seal("")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 空串编成密文会让「没设置」变成「已设置」，界面上就再也分不出这两件事。
	if sealed != "" {
		t.Errorf("空串被编成了 %q", sealed)
	}
}

func TestUnsealPlaintextPassesThrough(t *testing.T) {
	k := newKeyring(t)
	// 升级路径：加前缀之前手工写进库的值没有前缀，必须原样读出而不是报错。
	got, err := k.Unseal("明文老值")
	if err != nil {
		t.Fatalf("Unseal: %v", err)
	}
	if got != "明文老值" {
		t.Errorf("得到 %q", got)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	k := newKeyring(t)
	first, _ := k.Seal("同一个口令")
	second, _ := k.Seal("同一个口令")
	// nonce 复用是 GCM 上最严重的错，直接毁掉机密性；这条测试盯的就是它。
	if first == second {
		t.Error("两次加密同一明文得到相同密文，说明 nonce 没有随机化")
	}
}

func TestUnsealRejectsWrongKey(t *testing.T) {
	sealed, err := newKeyring(t).Seal("hunter2")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	other, err := New(make([]byte, keySize))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := other.Unseal(sealed); !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("换了一把密钥应得 ErrKeyMismatch，实际 %v", err)
	}
}

func TestUnsealRejectsTampered(t *testing.T) {
	k := newKeyring(t)
	sealed, _ := k.Seal("hunter2")

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, prefix))
	if err != nil {
		t.Fatalf("解 base64: %v", err)
	}
	raw[len(raw)-1] ^= 0xff // 翻掉密文最后一个字节
	tampered := prefix + base64.StdEncoding.EncodeToString(raw)

	// GCM 是认证加密：改一个比特也必须解不开，而不是解出一段垃圾。
	if _, err := k.Unseal(tampered); !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("被篡改的密文应得 ErrKeyMismatch，实际 %v", err)
	}
	if _, err := k.Unseal(prefix + "不是base64"); !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("坏 base64 应得 ErrKeyMismatch，实际 %v", err)
	}
	if _, err := k.Unseal(prefix); !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("只有前缀没有内容应得 ErrKeyMismatch，实际 %v", err)
	}
}

func TestOpenCreatesThenReusesKeyFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKey, "")

	first, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path := filepath.Join(dir, KeyFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("密钥文件没有生成: %v", err)
	}
	// Windows 上 Go 的 perm 位只有只读位有意义，这条断言留给 Linux/macOS。
	if info.Mode().Perm()&0o077 != 0 && filepath.Separator == '/' {
		t.Errorf("密钥文件权限为 %o，应当只有属主可读", info.Mode().Perm())
	}

	// 第二次打开必须拿到同一把：换一把的表现是「重启之后所有口令都解不开」，
	// 那是最难归因的一类故障。
	second, err := Open(dir)
	if err != nil {
		t.Fatalf("第二次 Open: %v", err)
	}
	sealed, err := first.Seal("hunter2")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := second.Unseal(sealed)
	if err != nil {
		t.Fatalf("第二次打开的密钥解不开第一次的密文: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("得到 %q", got)
	}
}

func TestOpenPrefersEnvKey(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, keySize)
	for i := range key {
		key[i] = byte(255 - i)
	}
	t.Setenv(EnvKey, base64.StdEncoding.EncodeToString(key))

	k, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, _ := want.Seal("hunter2")
	got, err := k.Unseal(sealed)
	if err != nil || got != "hunter2" {
		t.Errorf("环境变量给的密钥没有生效：got %q, err %v", got, err)
	}
	// 环境变量优先时不该顺手在工作目录里也生一把：多实例部署共用同一个 data 卷是常态，
	// 悄悄多出来的那把谁也不知道该不该备份。
	if _, err := os.Stat(filepath.Join(dir, KeyFileName)); !os.IsNotExist(err) {
		t.Error("指定了环境变量却仍然生成了密钥文件")
	}
}

func TestOpenRejectsBadEnvKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKey, "不是base64")
	if _, err := Open(dir); err == nil {
		t.Fatal("坏密钥应当报错而不是退回去生成一把新的——那会让已存的口令静默失效")
	}
}

func TestOpenRejectsBadKeyFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKey, "")
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("短了"), 0o600); err != nil {
		t.Fatalf("准备密钥文件: %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("密钥文件内容不合法时应当报错")
	}
}
