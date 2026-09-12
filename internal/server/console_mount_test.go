package server

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/FeiBaiKin/lumo/internal/console"
)

// TestConsoleBarePathRedirects 验证 /console（不带末尾斜杠）跳转到 /console/。
//
// chi 的 Mount 只对不以斜杠结尾的 pattern 额外注册裸路径，而 Console 的挂载点
// 天生带斜杠，所以 /console 一度没有任何 console 路由接住，直接漏给后注册的
// 兜底路由——生产环境里被主题的 /{slug} 当成独立页面，访客看到 404 页。
//
// 这里同时装上前台的兜底路由，确保断言的是真实遮蔽场景而非裸骨架的行为。
func TestConsoleBarePathRedirects(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{})
	// 模拟 theme.MountFrontend：/{slug} 吞掉根路径下的一切单段路径。
	front := chi.NewRouter()
	front.Get("/{slug}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	root.Mount("/", front)
	root.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := do(t, root, http.MethodGet, "/console", "")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("状态码 = %d，期望 301（/console 应跳转到 %s）", rec.Code, console.MountPath)
	}
	if loc := rec.Header().Get("Location"); loc != console.MountPath {
		t.Errorf("Location = %q，期望 %q", loc, console.MountPath)
	}

	// 带斜杠的规范路径必须照常命中 Console，不被重定向绕进去。
	rec = do(t, root, http.MethodGet, console.MountPath, "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotImplemented {
		t.Fatalf("状态码 = %d，期望 200（已构建前端）或 501（未构建）", rec.Code)
	}
}

// TestUploadsBarePathRedirects 验证 /uploads 与 /console 一样补了裸路径跳转。
//
// 挂载点同样带末尾斜杠，同样的漏接；附件访问虽以 /uploads/xxx 为主，
// 但不该让 /uploads 掉进前台的 /{slug}。
func TestUploadsBarePathRedirects(t *testing.T) {
	t.Parallel()

	root, _ := NewRouter(&Options{UploadsDir: t.TempDir()})
	rec := do(t, root, http.MethodGet, UploadsPath, "")

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("状态码 = %d，期望 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != UploadsPath+"/" {
		t.Errorf("Location = %q，期望 %q", loc, UploadsPath+"/")
	}
}
