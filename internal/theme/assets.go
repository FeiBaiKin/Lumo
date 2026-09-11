package theme

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// AssetsHandler 提供主题的 static 目录，挂在 AssetsPath 下。
//
// 路径形如 /theme-assets/<主题名>/<文件>：带上主题名而不是只服务当前启用主题，
// 是为了让后台预览其他主题时资源也能正确加载。
func (r *Registry) AssetsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rest := strings.TrimPrefix(req.URL.Path, AssetsPath+"/")
		name, file, ok := strings.Cut(rest, "/")
		if !ok || name == "" || file == "" {
			http.NotFound(w, req)
			return
		}

		loaded, exists := r.Get(name)
		if !exists || loaded.Static == nil {
			http.NotFound(w, req)
			return
		}

		clean := path.Clean("/" + file)[1:]
		if clean == "" || clean == "." || strings.HasPrefix(clean, "../") {
			http.NotFound(w, req)
			return
		}
		info, err := fs.Stat(loaded.Static, clean)
		if err != nil || info.IsDir() {
			// 目录不列出：主题的 static 里可能有作者没想给人看的中间产物。
			http.NotFound(w, req)
			return
		}

		// 主题静态资源与 Console 同源，且其中可能有 SVG——直接打开时脚本不得执行。
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// 主题资源随主题版本变化；开发模式下不缓存，否则改了 CSS 要手动强刷。
		if r.devMode {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		http.StripPrefix(AssetsPath+"/"+name, http.FileServerFS(loaded.Static)).ServeHTTP(w, req)
	})
}
