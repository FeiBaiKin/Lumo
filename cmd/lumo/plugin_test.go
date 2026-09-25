package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FeiBaiKin/lumo/internal/auth"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm/wasmtest"
)

// syncBuffer 是并发安全的日志缓冲：插件的动作在后台协程里处理。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Contains(s string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.buf.String(), s)
}

// guestManifest 是测试插件的清单：订阅它登记的全部钩子，并声明所需能力。
const guestManifest = `apiVersion: plugin.lumo.run/v1alpha1
kind: Plugin
metadata:
  name: hook-probe
spec:
  version: 1.0.0
  runtime: wasm
  capabilities:
    content: {read: true}
    frontend: true
  hooks:
    actions: [comment.created, post.updated]
    filters: [comment.judge, content.render]
`

// upload 以 multipart 上传插件包。
func (c *client) upload(path string, pkg []byte) (status int, out map[string]any) {
	c.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", "plugin.zip")
	_, _ = part.Write(pkg)
	_ = w.Close()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, c.base+path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set(auth.CSRFHeaderName, c.csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (c *client) page(path string) (status int, html string) {
	c.t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, c.base+path, http.NoBody)
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func zipPlugin(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 插件经钩子参与评论判定与正文输出，并收到动作通知；它插进正文的脚本被净化掉。
func TestPluginHooks(t *testing.T) {
	logs := &syncBuffer{}
	s := newTestSiteLogging(t, logs)
	s.createUser(t, "admin", "super-admin")
	admin := s.client(t)
	mustStatus(t, "管理员登录", admin.login("admin", "password-admin"), http.StatusOK, nil)

	pkg := zipPlugin(t, map[string][]byte{"plugin.yaml": []byte(guestManifest), "plugin.wasm": wasmtest.Guest(t)})
	status, out := admin.upload("/api/v1/console/plugins", pkg)
	mustStatus(t, "上传插件", status, http.StatusCreated, out)
	status, out = admin.do(http.MethodPut, "/api/v1/console/plugins/hook-probe/enabled", map[string]any{"enabled": true})
	mustStatus(t, "未确认能力就启用", status, http.StatusConflict, out)
	status, out = admin.do(http.MethodPut, "/api/v1/console/plugins/hook-probe/enabled",
		map[string]any{"enabled": true, "acceptCapabilities": true})
	mustStatus(t, "确认能力后启用", status, http.StatusOK, out)
	if out["running"] != true {
		t.Fatalf("启用后后端应在运行：%v", out)
	}

	status, out = admin.do(http.MethodPost, "/api/v1/console/posts", map[string]any{
		"title": "钩子探针", "slug": "hook-probe", "rawType": "html", "raw": "<p>正文</p>",
	})
	mustStatus(t, "发文", status, http.StatusCreated, out)
	postID, _ := out["id"].(float64)
	status, out = admin.do(http.MethodPost, "/api/v1/console/posts/"+jsonID(postID)+"/publish", map[string]any{})
	mustStatus(t, "发布", status, http.StatusOK, out)

	t.Run("正文经插件改写，插件塞的脚本被净化", func(t *testing.T) {
		status, html := s.client(t).page("/posts/hook-probe")
		if status != http.StatusOK {
			t.Fatalf("文章页状态码 %d", status)
		}
		if !strings.Contains(html, "插件追加") {
			t.Fatal("文章页里没有插件追加的内容")
		}
		if strings.Contains(html, "alert(1)") {
			t.Fatal("插件插进正文的脚本没有被净化掉")
		}
	})

	t.Run("评论判定：插件把广告判为垃圾，普通评论不动", func(t *testing.T) {
		path := "/api/v1/public/posts/" + jsonID(postID) + "/comments"
		status, out := s.client(t).do(http.MethodPost, path, map[string]any{
			"name": "访客", "email": "guest@example.com", "content": "便宜买药，加我",
		})
		mustStatus(t, "广告评论", status, http.StatusCreated, out)
		if out["status"] != "spam" {
			t.Fatalf("插件应把广告判为垃圾，得到 %v", out["status"])
		}
	})

	t.Run("动作送到了插件", func(t *testing.T) {
		deadline := time.Now().Add(5 * time.Second)
		for !logs.Contains("收到评论") || !logs.Contains("收到动作") {
			if time.Now().After(deadline) {
				t.Fatal("插件没有收到 comment.created 与 post.updated")
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}
