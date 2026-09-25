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
	"github.com/FeiBaiKin/lumo/internal/plugin"
	"github.com/FeiBaiKin/lumo/internal/plugin/wasm"
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
    cron: true
  cron:
    - {name: tick, every: 1h}
  hooks:
    actions: [comment.created, post.updated]
    filters: [comment.judge, content.render]
  resources:
    - kind: Note
      label: 笔记
      icon: notebook-pen
      columns: [title, score]
      schema:
        type: object
        x-order: [title, score]
        properties:
          title: {type: string, title: 标题, maxLength: 100}
          score: {type: integer, title: 分数, minimum: 0}
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

// installProbe 上传测试插件并确认能力后启用。
func installProbe(t *testing.T, admin *client) {
	t.Helper()
	installProbeWith(t, admin, guestManifest)
}

func installProbeWith(t *testing.T, admin *client, manifest string) {
	t.Helper()
	pkg := zipPlugin(t, map[string][]byte{"plugin.yaml": []byte(manifest), "plugin.wasm": wasmtest.Guest(t)})
	status, out := admin.upload("/api/v1/console/plugins", pkg)
	mustStatus(t, "上传插件", status, http.StatusCreated, out)
	status, out = admin.do(http.MethodPut, "/api/v1/console/plugins/hook-probe/enabled",
		map[string]any{"enabled": true, "acceptCapabilities": true})
	mustStatus(t, "确认能力后启用", status, http.StatusOK, out)
}

// 插件的数据：插件经宿主读写键值与资源，后台按声明增删改查，卸载时可以保留、重装接回，也可以一并删掉。
func TestPluginData(t *testing.T) {
	s := newTestSite(t)
	s.createUser(t, "admin", "super-admin")
	s.createUser(t, "editor", "editor")
	admin := s.client(t)
	mustStatus(t, "管理员登录", admin.login("admin", "password-admin"), http.StatusOK, nil)
	installProbe(t, admin)
	mod := plugin.From(s.site.application)

	t.Run("插件经宿主读写键值与资源记录", func(t *testing.T) {
		req := wasm.Request{Type: "action", Name: "post.updated", Payload: map[string]string{"mode": "data"}}
		if _, err := mod.Invoke(context.Background(), "hook-probe", req, 5*time.Second); err != nil {
			t.Fatalf("插件走查数据能力失败：%v", err)
		}
	})

	base := "/api/v1/console/plugins/hook-probe/resources"
	t.Run("后台按声明增删改查", func(t *testing.T) {
		status, out := admin.do(http.MethodGet, base, nil)
		mustStatus(t, "资源声明", status, http.StatusOK, out)
		items, _ := out["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["path"] != "notes" {
			t.Fatalf("应只声明了 notes：%v", out)
		}
		status, out = admin.do(http.MethodPost, base+"/notes", map[string]any{"data": map[string]any{"title": "后台写的", "score": 1}})
		mustStatus(t, "新建记录", status, http.StatusCreated, out)
		id, _ := out["id"].(float64)
		status, out = admin.do(http.MethodPost, base+"/notes", map[string]any{"data": map[string]any{"score": -1}})
		mustStatus(t, "不合声明的记录", status, http.StatusUnprocessableEntity, out)
		status, out = admin.do(http.MethodPut, base+"/notes/"+jsonID(id), map[string]any{"data": map[string]any{"title": "改过", "score": 9}})
		mustStatus(t, "修改记录", status, http.StatusOK, out)
		status, out = admin.do(http.MethodGet, base+"/notes?sort=-score", nil)
		mustStatus(t, "按分数倒序列出", status, http.StatusOK, out)
		records, _ := out["items"].([]any)
		if len(records) != 2 || records[0].(map[string]any)["data"].(map[string]any)["title"] != "改过" {
			t.Fatalf("按分数倒序时 9 分的应排第一：%v", out)
		}
		status, out = admin.do(http.MethodDelete, base+"/notes/"+jsonID(id), nil)
		mustStatus(t, "删除记录", status, http.StatusNoContent, out)
	})

	t.Run("资源页按声明的权限把关，侧栏出现入口", func(t *testing.T) {
		editor := s.client(t)
		mustStatus(t, "编辑登录", editor.login("editor", "password-editor"), http.StatusOK, nil)
		status, out := editor.do(http.MethodGet, base+"/notes", nil)
		mustStatus(t, "编辑看插件资源", status, http.StatusForbidden, out)

		status, out = admin.do(http.MethodGet, "/api/v1/console/navigation", nil)
		mustStatus(t, "取侧栏", status, http.StatusOK, out)
		raw, _ := json.Marshal(out)
		if !strings.Contains(string(raw), "/plugins/hook-probe/notes") {
			t.Fatalf("侧栏里没有插件的资源页入口：%s", raw)
		}
	})

	t.Run("卸载时保留数据，重装接回；缺省卸载一并删除", func(t *testing.T) {
		status, out := admin.do(http.MethodDelete, "/api/v1/console/plugins/hook-probe?keepData=true", nil)
		mustStatus(t, "保留数据卸载", status, http.StatusNoContent, out)
		status, out = admin.do(http.MethodGet, "/api/v1/console/plugins", nil)
		mustStatus(t, "插件列表", status, http.StatusOK, out)
		retained, _ := out["retained"].([]any)
		if len(retained) != 1 {
			t.Fatalf("应列出一份保留的数据：%v", out)
		}
		counts := retained[0].(map[string]any)["counts"].(map[string]any)
		if counts["kv"].(float64) < 2 || counts["records"].(float64) < 1 {
			t.Fatalf("保留的数据量不对：%v", counts)
		}

		installProbe(t, admin)
		status, out = admin.do(http.MethodGet, base+"/notes", nil)
		mustStatus(t, "重装后的记录", status, http.StatusOK, out)
		if out["total"].(float64) < 1 {
			t.Fatalf("重装后保留的记录应接回来：%v", out)
		}

		status, out = admin.do(http.MethodDelete, "/api/v1/console/plugins/hook-probe", nil)
		mustStatus(t, "缺省卸载", status, http.StatusNoContent, out)
		status, out = admin.do(http.MethodGet, "/api/v1/console/plugin-data/hook-probe", nil)
		mustStatus(t, "卸载后的数据量", status, http.StatusOK, out)
		if out["kv"].(float64) != 0 || out["records"].(float64) != 0 || out["settings"].(float64) != 0 {
			t.Fatalf("缺省卸载应把数据一并删掉：%v", out)
		}
	})
}

// fakeUpstream 把插件的对外请求接到本地：白名单核对照常发生，只是不真的出网。
type fakeUpstream struct{}

func (fakeUpstream) RoundTrip(req *http.Request) (*http.Response, error) {
	body := "not found"
	status := http.StatusNotFound
	if req.URL.Host == "api.example.com" && req.URL.Path == "/ping" {
		body, status = "pong", http.StatusOK
	}
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {"text/plain"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req,
	}, nil
}

// 插件经宿主读写站点内容、访问外部网络、跑定时任务；没被授予的写权限用不了。
func TestPluginCapabilities(t *testing.T) {
	s := newTestSite(t)
	s.createUser(t, "admin", "super-admin")
	admin := s.client(t)
	mustStatus(t, "管理员登录", admin.login("admin", "password-admin"), http.StatusOK, nil)
	granted := strings.Replace(guestManifest, "    content: {read: true}\n",
		"    content:\n      write: [posts:write, posts:write_any, posts:publish, comments:manage_any]\n"+
			"    http: [api.example.com]\n    mail: true\n", 1)
	installProbeWith(t, admin, granted)
	mod := plugin.From(s.site.application)
	mod.UseFetchTransport(fakeUpstream{})
	ctx := context.Background()
	run := func(mode string) error {
		req := wasm.Request{Type: "action", Name: "post.updated", Payload: map[string]string{"mode": mode}}
		_, err := mod.Invoke(ctx, "hook-probe", req, 10*time.Second)
		return err
	}

	t.Run("读写站点内容", func(t *testing.T) {
		if err := run("content"); err != nil {
			t.Fatalf("插件走查内容读写失败：%v", err)
		}
		status, html := s.client(t).page("/posts")
		if status != http.StatusOK || !strings.Contains(html, "插件改过的文章") {
			t.Fatalf("插件发的文章应出现在前台文章列表里（%d）", status)
		}
	})

	t.Run("访问外部网络只限白名单，发信要站点配好邮件", func(t *testing.T) {
		if err := run("fetch"); err != nil {
			t.Fatalf("插件走查外部网络失败：%v", err)
		}
	})

	t.Run("定时任务", func(t *testing.T) {
		if n := mod.RunDueJobs(ctx); n != 1 {
			t.Fatalf("应跑了 1 个到期任务，实际 %d", n)
		}
		if n := mod.RunDueJobs(ctx); n != 0 {
			t.Fatalf("同一周期里不该再跑，实际又跑了 %d 个", n)
		}
		if err := run("ticks"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("没授予的写权限用不了", func(t *testing.T) {
		status, out := admin.do(http.MethodDelete, "/api/v1/console/plugins/hook-probe", nil)
		mustStatus(t, "卸载", status, http.StatusNoContent, out)
		installProbe(t, admin)
		if err := run("content"); err == nil || !strings.Contains(err.Error(), "content.write") {
			t.Fatalf("没有写权限时发文章应被拒绝并说明该声明什么，得到 %v", err)
		}
	})
}
