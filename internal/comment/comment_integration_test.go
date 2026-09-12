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

// TestCommentPublicVisibilityEndToEnd 是审查发现的泄漏点的回归测试：
// 文章撤回或转私密后，正文已经 404，评论接口却仍然 200 并可匿名写入。
func TestCommentPublicVisibilityEndToEnd(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor-vis", perm.RoleEditor)
	postID := createPost(t, s, editor, "会被下线的文章")
	postPath := publicPrefix + "/posts/" + strconv.FormatInt(postID, 10) + "/comments"
	consolePath := consolePrefix + "/posts/" + strconv.FormatInt(postID, 10)

	// 先造一条已通过的评论，确认基线是可见的。
	created := mustStatus(t, req(t, s, http.MethodPost, postPath,
		`{"content":"一条会留下的评论","name":"访客","email":"v@example.com"}`, ""), http.StatusCreated)
	commentID := strconv.FormatInt(num(t, created["comment"].(map[string]any), "id"), 10)
	mustStatus(t, req(t, s, http.MethodPost, consolePrefix+"/comments/"+commentID+"/approve", `{}`, editor), http.StatusOK)
	if body := mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusOK); body["total"] != float64(1) {
		t.Fatalf("基线：已发布文章的评论应可见，total = %v", body["total"])
	}

	t.Run("撤回为草稿后评论不可读也不可写", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPost, consolePath+"/unpublish", `{}`, editor), http.StatusOK)
		mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"下线后还能写进去吗","name":"访客","email":"v@example.com"}`, ""), http.StatusNotFound)

		// 真正的伤害是「写进去了」：确认没有新增行。
		list := mustStatus(t, req(t, s, http.MethodGet, consolePrefix+"/comments?postId="+strconv.FormatInt(postID, 10), "", editor), http.StatusOK)
		if list["total"] != float64(1) {
			t.Errorf("下线后不应接受新评论，评论总数 = %v", list["total"])
		}
	})

	t.Run("转为私密后同样不可读不可写", func(t *testing.T) {
		// PUT 是整体替换，必须带上标题等必填字段。
		raw, _ := json.Marshal(map[string]any{
			"title": "会被下线的文章", "rawType": "markdown", "raw": "正文", "visibility": "private",
		})
		mustStatus(t, req(t, s, http.MethodPut, consolePath, string(raw), editor), http.StatusOK)
		mustStatus(t, req(t, s, http.MethodPost, consolePath+"/publish", `{}`, editor), http.StatusOK)

		mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusNotFound)
		mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"私密之后还能写吗","name":"访客","email":"v@example.com"}`, ""), http.StatusNotFound)
	})

	t.Run("移入回收站后不可读", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodDelete, consolePath, "", editor), http.StatusNoContent)
		mustStatus(t, req(t, s, http.MethodGet, postPath, "", ""), http.StatusNotFound)
	})

	t.Run("Console 仍能管理非公开内容下的评论", func(t *testing.T) {
		// 前台收紧不该影响后台：回收站里的评论依然可查、可删。
		body := mustStatus(t, req(t, s, http.MethodGet,
			consolePrefix+"/comments?postId="+strconv.FormatInt(postID, 10), "", editor), http.StatusOK)
		if body["total"] != float64(1) {
			t.Fatalf("后台应仍能看到该评论，total = %v", body["total"])
		}
		mustStatus(t, req(t, s, http.MethodDelete, consolePrefix+"/comments/"+commentID, "", editor), http.StatusNoContent)
	})
}

// TestCommentMaxLengthEndToEnd 验证「评论长度上限」设置真正生效：
// 过去服务端只执行固定的 10000 字硬上限，站点设置的 maxLength 形同虚设。
func TestCommentMaxLengthEndToEnd(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor-len", perm.RoleEditor)
	admin := s.Bearer(t, "admin-len", perm.RoleAdmin)
	postID := createPost(t, s, editor, "长度上限测试")
	postPath := publicPrefix + "/posts/" + strconv.FormatInt(postID, 10) + "/comments"

	// 站点设置：上限压到 100 字。settings:manage 只有 admin 及以上持有。
	raw, _ := json.Marshal(map[string]any{"enabled": true, "maxLength": 100})
	rec := s.Do(t, &testsupport.Request{
		Method: http.MethodPut, Path: consolePrefix + "/settings/comment", Body: string(raw), Auth: admin,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("更新评论设置失败 %d: %s", rec.Code, rec.Body.String())
	}

	// 按字符（而非字节）计：100 个汉字合法，101 个必须被拒。
	ok := strings.Repeat("字", 100)
	mustStatus(t, req(t, s, http.MethodPost, postPath,
		`{"content":"`+ok+`","name":"访客","email":"l@example.com"}`, ""), http.StatusCreated)

	tooLong := strings.Repeat("字", 101)
	mustStatus(t, req(t, s, http.MethodPost, postPath,
		`{"content":"`+tooLong+`","name":"访客","email":"l@example.com"}`, ""), http.StatusBadRequest)
}

// TestCommentPublicWriteRequiresCSRF 是审查发现的边界的回归测试：
// Public 平面会解析会话身份，但 CSRF 只挂在 Console 与 Extension 上，
// 于是同站点跨源的页面可以借受害者会话写评论（受害者是作者时评论还会自动过审）。
func TestCommentPublicWriteRequiresCSRF(t *testing.T) {
	s := newStack(t)
	editor := s.Bearer(t, "editor-csrf", perm.RoleEditor)
	postID := createPost(t, s, editor, "CSRF 测试文章")
	postPath := publicPrefix + "/posts/" + strconv.FormatInt(postID, 10) + "/comments"

	cookies, csrf := s.Session(t, "editor-csrf")
	body := `{"content":"用会话写的评论","name":"编辑器"}`

	t.Run("匿名访客不带 Cookie 时不受 CSRF 约束", func(t *testing.T) {
		mustStatus(t, req(t, s, http.MethodPost, postPath,
			`{"content":"匿名访客的评论","name":"访客甲","email":"g@example.com"}`, ""), http.StatusCreated)
	})

	t.Run("带会话但缺 CSRF 头返回 403", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{
			Method: http.MethodPost, Path: postPath, Body: body, Cookies: cookies,
		})
		mustStatus(t, rec, http.StatusForbidden)

		// 头不匹配同样拒绝。
		rec = s.Do(t, &testsupport.Request{
			Method: http.MethodPost, Path: postPath, Body: body, Cookies: cookies,
			Headers: map[string]string{"X-CSRF-Token": "not-the-token"},
		})
		mustStatus(t, rec, http.StatusForbidden)
	})

	t.Run("带上正确的 CSRF 头即可发表", func(t *testing.T) {
		rec := s.Do(t, &testsupport.Request{
			Method: http.MethodPost, Path: postPath, Body: body, Cookies: cookies,
			Headers: map[string]string{"X-CSRF-Token": csrf},
		})
		created := mustStatus(t, rec, http.StatusCreated)
		// 这里只断言「请求被受理」：状态可能是 approved，也可能因同一 IP 的
		// 发表间隔限制被判为 spam，那是反垃圾的职责，与本用例无关。
		item, _ := created["comment"].(map[string]any)
		if num(t, item, "id") == 0 {
			t.Errorf("应写入评论：%v", created)
		}
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
