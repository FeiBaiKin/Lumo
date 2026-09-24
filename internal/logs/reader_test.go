package logs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 日志下载按文件名取文件：只认日志目录里符合命名规则的文件，其余一律拒绝。
func TestOpenRejectsForeignNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lumo-2026-09-24.log"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.key"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReader(dir)

	f, _, err := r.Open("lumo-2026-09-24.log")
	if err != nil {
		t.Fatalf("合法的日志文件打不开：%v", err)
	}
	_ = f.Close()

	for _, name := range []string{
		"../secret.key", "..", "lumo-2026-09-24.log/../../secret.key", `..\secret.key`,
		"secret.key", "lumo-2026-09-24.txt", "lumo-latest.log", "",
	} {
		if f, _, err := r.Open(name); !errors.Is(err, ErrBadFileName) {
			if f != nil {
				_ = f.Close()
			}
			t.Errorf("文件名 %q 应被拒绝，得到 %v", name, err)
		}
	}
}
