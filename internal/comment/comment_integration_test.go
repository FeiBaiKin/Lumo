package comment_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/comment"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/mail"
	"github.com/FeiBaiKin/lumo/internal/migrate"
	"github.com/FeiBaiKin/lumo/internal/server"
	"github.com/FeiBaiKin/lumo/internal/settings"
	"github.com/FeiBaiKin/lumo/internal/taxonomy"
	"github.com/FeiBaiKin/lumo/internal/testsupport"
)

// 集成测试直连本机 PG 的独立测试库（agent.md §12），本包使用独占 schema。
const testSchema = "lumo_it_comment"

const (
	consolePrefix = server.PrefixConsole
	publicPrefix  = server.PrefixPublic
)

// newStack 按生产装配顺序装配 settings + mail + taxonomy + content + comment。
func newStack(t *testing.T) *testsupport.Stack {
	t.Helper()
	set, ml, tax, con, cmt := settings.New(), mail.New(), taxonomy.New(), content.New(), comment.New()
	db := testsupport.Open(t, testsupport.Options{
		Schema:  testSchema,
		Migrate: true,
		Sources: []migrate.Source{
			{Name: set.Name(), FS: set.Migrations()},
			{Name: tax.Name(), FS: tax.Migrations()},
			{Name: con.Name(), FS: con.Migrations()},
			{Name: cmt.Name(), FS: cmt.Migrations()},
		},
	})
	return testsupport.NewStack(t, db, set, ml, tax, con, cmt)
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码 = %d，期望 %d：%s", rec.Code, want, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func req(t *testing.T, s *testsupport.Stack, method, path, body, authz string) *httptest.ResponseRecorder {
	t.Helper()
	return s.Do(t, &testsupport.Request{Method: method, Path: path, Body: body, Auth: authz})
}

func num(t *testing.T, body map[string]any, key string) int64 {
	t.Helper()
	v, ok := body[key].(float64)
	if !ok {
		t.Fatalf("响应缺少 %s：%v", key, body)
	}
	return int64(v)
}

// createPost 用 editor 建一篇已发布文章，返回其 ID。
func createPost(t *testing.T, s *testsupport.Stack, bearer, title string) int64 {
	t.Helper()
	body := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts",
		`{"title":"`+title+`","raw":"正文","rawType":"markdown"}`, bearer), http.StatusCreated)
	id := num(t, body, "id")
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/posts/"+strconv.FormatInt(id, 10)+"/publish",
		`{}`, bearer), http.StatusOK)
	return id
}

func TestCommentEndToEnd(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor", perm.RoleEditor)
	authorRole := s.Bearer(t, "author", perm.RoleAuthor)

	postID := createPost(t, s, editor, "评论测试文章")
	postPath := publicPrefix + "/posts/" + strconv.FormatInt(postID, 10) + "/comments"

	var topID string

	t.Run("访客发表评论需审核且内容被转义", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"第一条 <script>alert(1)</script>","name":"访客甲","email":"guest@example.com"}`,
			""), http.StatusCreated)

		if body["status"] != string(comment.StatusPending) {
			t.Errorf("默认应待审，实际 %v", body["status"])
		}
		item, _ := body["comment"].(map[string]any)
		topID = strconv.FormatInt(num(t, item, "id"), 10)

		html, _ := item["contentHtml"].(string)
		if strings.Contains(html, "<script") {
			t.Fatalf("评论 HTML 泄漏了脚本标签：%s", html)
		}
		if !strings.Contains(html, "&lt;script&gt;") {
			t.Errorf("应输出转义后的内容：%s", html)
		}
	})

	t.Run("待审评论不出现在前台", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusOK)
		if body["total"] != float64(0) {
			t.Errorf("待审评论不应出现在前台，total = %v", body["total"])
		}
	})

	t.Run("审核通过后前台可见且不含个人信息", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/comments/"+topID+"/approve", `{}`, editor), http.StatusOK)

		body := mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusOK)
		if body["total"] != float64(1) {
			t.Fatalf("通过后应可见，total = %v", body["total"])
		}
		raw, _ := json.Marshal(body)
		for _, leaked := range []string{"guest@example.com", "authorEmail", "\"ip\"", "userAgent"} {
			if strings.Contains(string(raw), leaked) {
				t.Errorf("前台响应泄漏了 %s：%s", leaked, raw)
			}
		}
	})

	t.Run("回复形成树，回复待审的评论被拒", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"回复上面那条","name":"访客乙","email":"b@example.com","parentId":`+topID+`}`,
			""), http.StatusCreated)
		reply, _ := body["comment"].(map[string]any)
		replyID := strconv.FormatInt(num(t, reply, "id"), 10)
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/comments/"+replyID+"/approve", `{}`, editor), http.StatusOK)

		tree := mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusOK)
		items, _ := tree["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("顶层评论应有 1 条，实际 %d", len(items))
		}
		top, _ := items[0].(map[string]any)
		children, _ := top["children"].([]any)
		if len(children) != 1 {
			t.Errorf("应挂一条回复，实际 %d 条", len(children))
		}

		// 回复一条待审的评论应被拒。
		pending := mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"待审的一条","name":"访客丙","email":"c@example.com"}`, ""), http.StatusCreated)
		p, _ := pending["comment"].(map[string]any)
		pendingID := strconv.FormatInt(num(t, p, "id"), 10)
		mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"回复待审的","name":"访客丁","email":"d@example.com","parentId":`+pendingID+`}`,
			""), http.StatusBadRequest)
	})

	t.Run("权限：作者只管自己内容下的评论", func(t *testing.T) {
		// author 角色有 comments:manage 但没有 _any。
		other := createPost(t, s, editor, "别人的文章")
		otherPath := publicPrefix + "/posts/" + strconv.FormatInt(other, 10) + "/comments"
		c := mustStatus(t, req(t, s, http.MethodPost, otherPath,
			`{"content":"别人的评论","name":"路人","email":"x@example.com"}`, ""), http.StatusCreated)
		item, _ := c["comment"].(map[string]any)
		otherID := strconv.FormatInt(num(t, item, "id"), 10)

		// author 看不到、也审不了别人文章下的评论。
		list := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/comments", "", authorRole), http.StatusOK)
		if list["total"] != float64(0) {
			t.Errorf("author 不应看到他人内容下的评论，total = %v", list["total"])
		}
		mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/comments/"+otherID+"/approve", `{}`, authorRole),
			http.StatusForbidden)
	})

	t.Run("垃圾判定：蜜罐与关键词直接落为 spam", func(t *testing.T) {
		honeypot := mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"正常内容","name":"机器人","email":"r@example.com","website2":"http://spam.example"}`,
			""), http.StatusCreated)
		if honeypot["status"] != string(comment.StatusSpam) {
			t.Errorf("蜜罐被填应判为垃圾，实际 %v", honeypot["status"])
		}
	})

	t.Run("管理员回复直接通过", func(t *testing.T) {
		body := mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/comments/"+topID+"/replies",
			`{"content":"站长的回复"}`, editor), http.StatusCreated)
		if body["status"] != string(comment.StatusApproved) {
			t.Errorf("管理员回复应直接通过，实际 %v", body["status"])
		}
	})

	t.Run("删除父评论连带删除回复", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/comments/"+topID, "", editor), http.StatusNoContent)
		tree := mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusOK)
		if tree["total"] != float64(0) {
			t.Errorf("父评论删除后其回复也应消失，total = %v", tree["total"])
		}
	})

	t.Run("鉴权与校验", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/comments", "", ""), http.StatusUnauthorized)
		// author 角色没有 posts:publish 之外的评论权限问题，这里验证无权限用户被拒。
		mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusOK)
		// 空内容与不存在的文章。
		mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"   ","name":"甲","email":"a@example.com"}`, ""), http.StatusBadRequest)
		mustStatus(t, req(t, s, http.MethodPost, publicPrefix+"/posts/999999/comments",
			`{"content":"你好","name":"甲","email":"a@example.com"}`, ""), http.StatusNotFound)
		// javascript: 主页地址被拒。
		mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"你好","name":"甲","email":"a@example.com","url":"javascript:alert(1)"}`,
			""), http.StatusBadRequest)
	})
}

// TestCommentSettingsPublic 验证前台能读到评论的公开设置。
func TestCommentSettingsPublic(t *testing.T) {
	s := newStack(t)
	body := mustStatus(t, req(t, s, http.MethodGet, publicPrefix+"/settings", "", ""), http.StatusOK)
	site, ok := body["comment"].(map[string]any)
	if !ok {
		t.Fatalf("公开设置应包含 comment 分组：%v", body)
	}
	if site["enabled"] != true {
		t.Errorf("评论默认应开启，实际 %v", site["enabled"])
	}
	if _, leaked := site["blocklist"]; leaked {
		t.Errorf("黑名单不属于公开字段：%v", site)
	}
	if _, leaked := site["notifyTo"]; leaked {
		t.Errorf("通知地址不属于公开字段：%v", site)
	}
}
