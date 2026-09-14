package media_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/app"
	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/config"
	"github.com/FeiBaiKin/lumo/internal/media"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_media"

const consolePrefix = server.PrefixConsole

// stack 是一套装好的测试栈，附带本次测试使用的工作目录。
type stack struct {
	*testsupport.Stack
	dataDir string
}

// uploadsDir 返回本地存储的根目录。
func (s *stack) uploadsDir() string {
	return filepath.Join(s.dataDir, media.UploadsDirName)
}

// newStack 按生产装配顺序装配 settings + media，工作目录指向临时目录。
func newStack(t *testing.T) *stack {
	t.Helper()
	set, med := settings.New(), media.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: med.Name(), FS: med.Migrations()},
		},
	})

	dataDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dataDir

	inner := testsupport.NewStackWith(t, db, &testsupport.StackOptions{
		Config:     cfg,
		UploadsDir: filepath.Join(dataDir, media.UploadsDirName),
		Modules:    []app.Module{set, med},
	})
	return &stack{Stack: inner, dataDir: dataDir}
}

// ---------- 请求辅助 ----------

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	return decode(t, rec)
}

func req(t *testing.T, s *stack, method, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: auth})
}

// upload 以 multipart 提交一个文件。
func upload(t *testing.T, s *stack, filename string, content []byte, alt, title, auth string) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("写入 multipart 内容失败: %v", err)
	}
	if alt != "" {
		_ = writer.WriteField("alt", alt)
	}
	if title != "" {
		_ = writer.WriteField("title", title)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}

	return s.Do(t, &testsupport.Request{
		Method:      http.MethodPost,
		Path:        consolePrefix + "/media",
		Body:        buf.String(),
		ContentType: writer.FormDataContentType(),
		Auth:        auth,
	})
}

// pngBytes 生成一张纯色 PNG。
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x40, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成测试 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

func id(t *testing.T, body map[string]any) string {
	t.Helper()
	v, ok := body["id"].(float64)
	if !ok {
		t.Fatalf("响应缺少 id：%v", body)
	}
	return strconv.FormatInt(int64(v), 10)
}

// ---------- 测试 ----------

func TestMediaEndToEnd(t *testing.T) {
	s := newStack(t)
	author := s.Bearer(t, "author", perm.RoleAuthor)
	editor := s.Bearer(t, "editor", perm.RoleEditor)

	var photoID string

	t.Run("上传图片生成缩略图并落盘", func(t *testing.T) {
		rec := upload(t, s, "我的照片.png", pngBytes(t, 1000, 500), "一张测试图", "照片", author)
		body := mustStatus(t, rec, http.StatusCreated)
		photoID = id(t, body)

		if body["mime"] != "image/png" || body["kind"] != "image" {
			t.Errorf("类型判定错误：%v / %v", body["mime"], body["kind"])
		}
		if body["originalName"] != "我的照片.png" {
			t.Errorf("原始名应保留，实际 %v", body["originalName"])
		}
		if body["width"] != float64(1000) || body["height"] != float64(500) {
			t.Errorf("尺寸应为 1000×500，实际 %v×%v", body["width"], body["height"])
		}
		if body["driver"] != media.DriverLocal {
			t.Errorf("驱动应为 local，实际 %v", body["driver"])
		}
		if body["alt"] != "一张测试图" || body["title"] != "照片" {
			t.Errorf("表单字段未落库：%v / %v", body["alt"], body["title"])
		}
		if checksum, _ := body["checksum"].(string); len(checksum) != 64 {
			t.Errorf("校验和应为 64 位十六进制，实际 %q", checksum)
		}

		// 存储用的文件名必须是随机化的，不能带上原始中文名。
		filename, _ := body["filename"].(string)
		if strings.Contains(filename, "我的照片") || !strings.HasSuffix(filename, ".png") {
			t.Errorf("文件名应随机化，实际 %q", filename)
		}
		key, _ := body["storageKey"].(string)
		if !strings.HasSuffix(key, filename) || strings.Contains(key, "..") {
			t.Errorf("对象键异常：%q", key)
		}
		if url, _ := body["url"].(string); url != media.UploadsURLPrefix+"/"+key {
			t.Errorf("URL 应为 %s/%s，实际 %v", media.UploadsURLPrefix, key, body["url"])
		}

		// 原图与两档缩略图都应真正落到磁盘上。
		if _, err := os.Stat(filepath.Join(s.uploadsDir(), filepath.FromSlash(key))); err != nil {
			t.Errorf("原图未落盘: %v", err)
		}
		thumbs, _ := body["thumbnails"].([]any)
		if len(thumbs) != 2 {
			t.Fatalf("应生成 2 档缩略图，实际 %d 档：%v", len(thumbs), body["thumbnails"])
		}
		for _, raw := range thumbs {
			thumb, _ := raw.(map[string]any)
			thumbKey, _ := thumb["key"].(string)
			if !strings.HasSuffix(thumbKey, ".webp") {
				t.Errorf("缩略图应为 WebP，实际 %q", thumbKey)
			}
			if _, err := os.Stat(filepath.Join(s.uploadsDir(), filepath.FromSlash(thumbKey))); err != nil {
				t.Errorf("缩略图 %s 未落盘: %v", thumbKey, err)
			}
		}

		if uploader, _ := body["uploader"].(map[string]any); uploader == nil || uploader["username"] != "author" {
			t.Errorf("响应应带上传者信息，实际 %v", body["uploader"])
		}
	})

	t.Run("经 /uploads 可取回文件且带防护响应头", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media/"+photoID, "", author), http.StatusOK)
		url, _ := body["url"].(string)

		rec := s.Do(t, &testsupport.Request{Method: http.MethodGet, Path: url})
		if rec.Code != http.StatusOK {
			t.Fatalf("取回附件状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("应带 nosniff，实际 %q", got)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
			t.Errorf("应带 sandbox 的 CSP，实际 %q", csp)
		}
	})

	t.Run("列表按分类与上传者筛选", func(t *testing.T) {
		if rec := upload(t, s, "说明.txt", []byte("纯文本内容"), "", "", editor); rec.Code != http.StatusCreated {
			t.Fatalf("上传文本失败：%d %s", rec.Code, rec.Body.String())
		}

		all := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media", "", author), http.StatusOK)
		if all["total"] != float64(2) {
			t.Errorf("总数应为 2，实际 %v", all["total"])
		}

		images := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media?kind=image", "", author), http.StatusOK)
		if images["total"] != float64(1) {
			t.Errorf("图片数应为 1，实际 %v", images["total"])
		}

		docs := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media?kind=document", "", author), http.StatusOK)
		if docs["total"] != float64(1) {
			t.Errorf("文档数应为 1，实际 %v", docs["total"])
		}

		byName := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media?q=说明", "", author), http.StatusOK)
		if byName["total"] != float64(1) {
			t.Errorf("按名称筛选应命中 1 条，实际 %v", byName["total"])
		}
	})

	t.Run("类型不合法一律拒绝", func(t *testing.T) {
		cases := []struct {
			name    string
			file    string
			content []byte
			want    int
		}{
			{"扩展名不在白名单", "payload.exe", []byte("MZ\x90\x00"), http.StatusUnsupportedMediaType},
			{"HTML 伪装成图片", "evil.png", []byte("<!DOCTYPE HTML><html><script>alert(1)</script></html>"), http.StatusUnsupportedMediaType},
			{"PNG 配错扩展名", "mismatch.jpg", pngBytes(t, 8, 8), http.StatusUnsupportedMediaType},
		}
		for _, c := range cases {
			rec := upload(t, s, c.file, c.content, "", "", author)
			if rec.Code != c.want {
				t.Errorf("%s：状态码 = %d，期望 %d：%s", c.name, rec.Code, c.want, rec.Body.String())
			}
		}
		// 被拒的上传不应在库里留记录。
		body := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media", "", author), http.StatusOK)
		if body["total"] != float64(2) {
			t.Errorf("被拒的上传不应落库，总数 = %v", body["total"])
		}
	})

	t.Run("所有权：他人的附件改不了也删不掉", func(t *testing.T) {
		// author 改自己的可以。
		body := mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/media/"+photoID,
			`{"alt":"改过的说明","title":"新标题"}`, author), http.StatusOK)
		if body["alt"] != "改过的说明" || body["title"] != "新标题" {
			t.Errorf("更新未生效：%v / %v", body["alt"], body["title"])
		}

		// editor 上传的那条，author 既改不了也删不了。
		list := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media?kind=document", "", author), http.StatusOK)
		items, _ := list["items"].([]any)
		other, _ := items[0].(map[string]any)
		otherID := id(t, other)

		forbidden := mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/media/"+otherID,
			`{"alt":"越权"}`, author), http.StatusForbidden)
		required, _ := forbidden["requiredPermissions"].([]any)
		if len(required) != 1 || required[0] != perm.MediaDeleteAny.String() {
			t.Errorf("应列出所需权限 %s，实际 %v", perm.MediaDeleteAny, forbidden["requiredPermissions"])
		}
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/media/"+otherID, "", author), http.StatusForbidden)

		// editor 持有 media:delete_any，可以改也可以删 author 的附件。
		mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/media/"+photoID,
			`{"alt":"编辑改的"}`, editor), http.StatusOK)
	})

	t.Run("删除同时清掉原文件与缩略图", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media/"+photoID, "", author), http.StatusOK)
		keys := []string{body["storageKey"].(string)}
		for _, raw := range body["thumbnails"].([]any) {
			thumb, _ := raw.(map[string]any)
			keys = append(keys, thumb["key"].(string))
		}

		rec := req(t, s, http.MethodDelete, consolePrefix+"/media/"+photoID, "", author)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("删除状态码 = %d：%s", rec.Code, rec.Body.String())
		}
		for _, key := range keys {
			if _, err := os.Stat(filepath.Join(s.uploadsDir(), filepath.FromSlash(key))); !os.IsNotExist(err) {
				t.Errorf("文件 %s 应已删除", key)
			}
		}
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media/"+photoID, "", author), http.StatusNotFound)
	})

	t.Run("鉴权", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media", "", ""), http.StatusUnauthorized)
		if rec := upload(t, s, "a.png", pngBytes(t, 8, 8), "", "", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("匿名上传状态码 = %d，期望 401", rec.Code)
		}
	})

	t.Run("未知附件返回 404", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/media/999999", "", author), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/media/999999", "", editor), http.StatusNotFound)
	})
}

// TestMediaStorageSettings 验证存储设置分组随模块注册，且密钥字段不回传明文。
func TestMediaStorageSettings(t *testing.T) {
	s := newStack(t)
	admin := s.Bearer(t, "admin", perm.RoleAdmin)

	body := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings/storage", "", admin), http.StatusOK)
	values, _ := body["values"].(map[string]any)
	if values["driver"] != media.DriverLocal {
		t.Errorf("默认驱动应为 local，实际 %v", values["driver"])
	}
	// 两个密钥字段是 2026-09-14 加进来的：此前「设置里不许有密钥字段」，
	// 那条规则把密钥和站长一起挡在了后台外面（agent.md §9）。现在要守的是
	// 「字段在、值恒空」——后台填得进去，接口吐不出来。
	for _, key := range []string{"s3AccessKey", "s3SecretKey"} {
		if v, ok := values[key]; !ok {
			t.Errorf("密钥字段 %q 不在设置里，后台上就填不了", key)
		} else if v != "" {
			t.Errorf("%s 回传了值：%v", key, v)
		}
	}
	if set, _ := body["secretSet"].([]any); len(set) != 0 {
		t.Errorf("尚未设置任何密钥，secretSet 应为空，实际 %v", set)
	}

	// 选 s3 却不填地址与桶名，应逐条报错且不改动已保存值。
	rejected := mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/storage",
		`{"driver":"s3"}`, admin), http.StatusUnprocessableEntity)
	details, _ := rejected["errors"].([]any)
	if len(details) != 2 {
		t.Errorf("应有 2 条明细（地址与桶名），实际 %v", rejected["errors"])
	}

	after := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/settings/storage", "", admin), http.StatusOK)
	if v, _ := after["values"].(map[string]any); v["driver"] != media.DriverLocal {
		t.Errorf("校验失败不应改动已保存值，实际 %v", v["driver"])
	}

	// 填上密钥：响应里仍是空串，另由 secretSet 说明「已设置」。
	// 界面靠这条区分「没存过」和「存过但没显示」——两者在输入框里长得一样，
	// 而该做的下一步正好相反。
	saved := mustStatus(t, req(t, s, http.MethodPut, consolePrefix+"/settings/storage",
		`{"driver":"local","s3AccessKey":"AKIA_TEST","s3SecretKey":"secret_test"}`, admin), http.StatusOK)
	if v, _ := saved["values"].(map[string]any); v["s3SecretKey"] != "" {
		t.Errorf("保存响应回传了密钥：%v", v)
	}
	set, _ := saved["secretSet"].([]any)
	if len(set) != 2 {
		t.Errorf("secretSet = %v，期望两个密钥都在里面", set)
	}
}
