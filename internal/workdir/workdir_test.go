package workdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesAllSubdirs(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "data")
	abs, err := Init(root, nil)
	if err != nil {
		t.Fatalf("Init 返回错误: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("应返回绝对路径，实际 %q", abs)
	}

	for _, name := range Subdirs {
		info, statErr := os.Stat(filepath.Join(abs, name))
		if statErr != nil {
			t.Errorf("子目录 %s 未创建: %v", name, statErr)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s 不是目录", name)
		}
	}
}

// TestInitIsIdempotent 验证重复调用不报错——每次启动都会执行它。
func TestInitIsIdempotent(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "data")
	if _, err := Init(root, nil); err != nil {
		t.Fatalf("首次 Init 失败: %v", err)
	}
	if _, err := Init(root, nil); err != nil {
		t.Fatalf("重复 Init 应当成功: %v", err)
	}
}

// TestInitPreservesExistingContent 验证已有文件不被清除。
func TestInitPreservesExistingContent(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "data")
	if _, err := Init(root, nil); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	marker := filepath.Join(root, "uploads", "keep.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatalf("写入标记文件失败: %v", err)
	}
	if _, err := Init(root, nil); err != nil {
		t.Fatalf("重复 Init 失败: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("已有文件被破坏: %v", err)
	}
}

func TestInitRejectsEmptyRoot(t *testing.T) {
	t.Parallel()

	if _, err := Init("", nil); err == nil {
		t.Fatal("空根目录应报错")
	}
}

// TestInitRejectsFileCollision 验证同名路径被普通文件占用时给出明确错误，
// 而不是让后续写入以难懂的方式失败。
func TestInitRejectsFileCollision(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("创建冲突文件失败: %v", err)
	}
	if _, err := Init(path, nil); err == nil {
		t.Fatal("路径被文件占用时应报错")
	}
}
