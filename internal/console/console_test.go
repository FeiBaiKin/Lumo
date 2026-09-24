package console

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAHandlerGzip(t *testing.T) {
	script := strings.Repeat("console.log('lumo');\n", 200)
	assets := fstest.MapFS{
		"index.html":          {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/index-abc.js": {Data: []byte(script)},
		"assets/logo-abc.png": {Data: []byte("\x89PNG not really")},
	}
	h := spaHandler(assets)

	get := func(target, encoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
		if encoding != "" {
			req.Header.Set("Accept-Encoding", encoding)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("/assets/index-abc.js", "gzip, br")
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(script) {
		t.Fatalf("接受 gzip 的客户端应拿到压缩过的脚本：%v，%d 字节", rec.Header(), rec.Body.Len())
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if string(plain) != script {
		t.Fatal("解压后与原文件不一致")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("压缩过的响应必须带 Vary: Accept-Encoding，否则共享缓存会把 gzip 发给不支持的客户端")
	}

	for _, enc := range []string{"", "identity", "gzip;q=0"} {
		if rec := get("/assets/index-abc.js", enc); rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != script {
			t.Errorf("Accept-Encoding=%q 时应发原文件", enc)
		}
	}
	if rec := get("/assets/logo-abc.png", "gzip"); rec.Header().Get("Content-Encoding") != "" {
		t.Error("图片本身已是压缩格式，不该再压")
	}
	if rec := get("/posts/12", "gzip"); !strings.Contains(rec.Body.String(), "id=root") {
		t.Error("前端路由应回退到 index.html")
	}
	if rec := get("/assets/missing.js", "gzip"); rec.Code != http.StatusNotFound {
		t.Errorf("缺失的静态资源应 404，得到 %d", rec.Code)
	}
}
