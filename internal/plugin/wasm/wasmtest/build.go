// Package wasmtest 给测试编译插件：用 Go SDK 现场编一个 wasip1 模块。
package wasmtest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	once  sync.Once
	built []byte
	fail  string
)

// Guest 返回测试插件（testdata/guest）编译出的 plugin.wasm，同一进程里只编一次。
//
// -short 时跳过：编译 wasm 要十几秒，本地快速跑单元测试时不该每次都等。
func Guest(t testing.TB) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("编译测试插件较慢，-short 时跳过")
	}
	once.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		dir := filepath.Join(filepath.Dir(file), "..", "testdata", "guest")
		// 编完立刻读进内存，临时目录随第一个测试结束被清掉也无妨
		out := filepath.Join(t.TempDir(), "plugin.wasm")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-buildmode=c-shared", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "GOFLAGS=", "GOWORK=off", "CGO_ENABLED=0")
		if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
			fail = "编译测试插件失败：" + buildErr.Error() + "\n" + string(output)
			return
		}
		data, readErr := os.ReadFile(out)
		if readErr != nil {
			fail = readErr.Error()
			return
		}
		built = data
	})
	if fail != "" {
		t.Fatal(fail)
	}
	return built
}
