// Package console 把 Vite 构建出的 Console SPA 嵌入二进制并提供静态资源服务。
//
// dist 目录由 `task console:build` 生成（见 Taskfile.yml），不入库。
// 仓库只保留 dist/.gitkeep 占位，保证未构建前端时后端依然可以编译。
//
// 本包的 Handler 期望被挂载在 MountPath 下，调用方需自行 StripPrefix。
package console

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// MountPath 是 Console 的挂载前缀，必须与 console/vite.config.ts 的 base 一致。
const MountPath = "/console/"

// all: 前缀确保包含 .gitkeep 这类以点开头的文件，
// 否则未构建前端时 embed 会因「no matching files」而编译失败。
//
//go:embed all:dist
var distFS embed.FS

// Assets 返回以 dist 为根的文件系统。
func Assets() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Built 报告 Console 前端产物是否已嵌入（即 dist/index.html 是否存在）。
// 未构建时后端仍可启动，只是 Console 路由返回提示页。
func Built() bool {
	assets, err := Assets()
	if err != nil {
		return false
	}
	_, err = fs.Stat(assets, "index.html")
	return err == nil
}

// Handler 返回 Console SPA 的静态资源处理器（已含 MountPath 前缀剥离）。
//
// 行为：命中真实文件则直接返回；未命中且请求不带扩展名时回退到 index.html，
// 以支持前端的 history 路由。前端未构建时统一返回 501 提示。
func Handler() http.Handler {
	assets, err := Assets()
	if err != nil || !Built() {
		return http.HandlerFunc(notBuilt)
	}
	// MountPath 末尾带斜杠，StripPrefix 需去掉它才能正确处理 /console/ 自身。
	return http.StripPrefix(strings.TrimSuffix(MountPath, "/"), spaHandler(assets))
}

// spaHandler 在给定文件系统上实现 SPA 静态服务与 history 路由回退。
func spaHandler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	compressed := &gzipCache{assets: assets, files: map[string][]byte{}}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if _, statErr := fs.Stat(assets, name); statErr != nil {
			if !errors.Is(statErr, fs.ErrNotExist) {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			// 带扩展名的请求视为静态资源，缺失即 404，不回退到 index.html，
			// 否则前端会把 HTML 当作 JS 解析而报错。
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, assets)
			return
		}

		// 带内容哈希的构建产物可长期缓存；index.html 必须每次回源校验。
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			if compressible(name) {
				w.Header().Add("Vary", "Accept-Encoding")
				if acceptsGzip(r) {
					if data, ok := compressed.get(name); ok {
						serveGzip(w, r, name, data)
						return
					}
				}
			}
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// gzipCache 缓存压缩过的构建产物。
//
// 后台首屏的脚本未压缩时有六百多 KB，不经反向代理直接部署的站点每个新访客都要全额下载。
// 产物嵌在二进制里、内容永不变，所以每个文件只在第一次被请求时压一次，之后直接发压好的字节。
type gzipCache struct {
	assets fs.FS
	mu     sync.Mutex
	files  map[string][]byte
}

// get 返回 name 的 gzip 字节；读不出或压完不比原文件小时返回 false，由调用方发原文件。
func (c *gzipCache) get(name string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if data, ok := c.files[name]; ok {
		return data, data != nil
	}
	raw, err := fs.ReadFile(c.assets, name)
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, false
	}
	if _, err := zw.Write(raw); err != nil || zw.Close() != nil || buf.Len() >= len(raw) {
		c.files[name] = nil
		return nil, false
	}
	c.files[name] = buf.Bytes()
	return c.files[name], true
}

// compressible 报告这类产物值不值得压缩：图片与字体本身已经是压缩格式。
func compressible(name string) bool {
	switch path.Ext(name) {
	case ".js", ".css", ".svg", ".json", ".map":
		return true
	}
	return false
}

// acceptsGzip 报告客户端是否接受 gzip。显式写了 q=0 的视为拒绝。
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			return strings.ReplaceAll(strings.TrimSpace(params), " ", "") != "q=0"
		}
	}
	return false
}

// serveGzip 发出压缩过的产物。
func serveGzip(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if ctype := mime.TypeByExtension(path.Ext(name)); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

// serveIndex 输出 SPA 入口文件，供 history 路由回退使用。
func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

// notBuilt 在 Console 产物缺失时给出可操作的提示，而非空白页。
func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte("Console 前端尚未构建。请先执行：task console:build\n"))
}
