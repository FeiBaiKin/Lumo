// Package console 把 Vite 构建出的 Console SPA 嵌入二进制并提供静态资源服务。
//
// dist 目录由 `task console:build` 生成（见 Taskfile.yml），不入库。
// 仓库只保留 dist/.gitkeep 占位，保证未构建前端时后端依然可以编译。
//
// 本包的 Handler 期望被挂载在 MountPath 下，调用方需自行 StripPrefix。
package console

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
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
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
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
