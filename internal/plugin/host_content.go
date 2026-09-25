package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/FeiBaiKin/lumo/internal/auth/perm"
	"github.com/FeiBaiKin/lumo/internal/comment"
	"github.com/FeiBaiKin/lumo/internal/content"
	"github.com/FeiBaiKin/lumo/internal/hooks"
)

// 站点内容的读写。读要被授予 content.read；写按 content.write 里授予的权限串逐项判定。
//
// 读是插件模块自己的只读查询（与主题读前台数据是同一种做法）；写转给内容与评论模块，
// 渲染、净化、slug 与动作派发都走和后台一样的代码，插件绕不开那些规则。
func init() {
	read := func(g *Capabilities) bool { return g.Content.Read }
	write := func(g *Capabilities) bool { return len(g.Content.Write) > 0 }
	const needRead = "在 capabilities.content 里声明 read: true"
	const needWrite = "在 capabilities.content.write 里声明所需的权限串"
	for name, op := range map[string]hostOp{
		"content.posts.list":        {allowed: read, denied: needRead, call: hostPostsList},
		"content.posts.get":         {allowed: read, denied: needRead, call: hostPostsGet},
		"content.comments.list":     {allowed: read, denied: needRead, call: hostCommentsList},
		"content.comments.get":      {allowed: read, denied: needRead, call: hostCommentsGet},
		"content.users.get":         {allowed: read, denied: needRead, call: hostUsersGet},
		"content.terms.list":        {allowed: read, denied: needRead, call: hostTermsList},
		"content.posts.create":      {allowed: write, denied: needWrite, call: hostPostsCreate},
		"content.posts.update":      {allowed: write, denied: needWrite, call: hostPostsUpdate},
		"content.posts.trash":       {allowed: write, denied: needWrite, call: hostPostsTrash},
		"content.comments.moderate": {allowed: write, denied: needWrite, call: hostCommentsModerate},
		"content.comments.delete":   {allowed: write, denied: needWrite, call: hostCommentsDelete},
	} {
		hostOps[name] = op
	}
}

// maxContentPage 是一次读出的内容条数上限。
const maxContentPage = 100

// requireWrite 核对插件被授予了某个写权限。
func requireWrite(loaded *Loaded, p perm.Permission) error {
	if loaded.Granted == nil || !loaded.Granted.AllowsWrite(p) {
		return fmt.Errorf("没有 %s 权限：请在 capabilities.content.write 里声明它", p)
	}
	return nil
}

func (m *Module) readDB() (*bun.DB, error) {
	if m.db == nil {
		return nil, errors.New("站点没有连接数据库")
	}
	return m.db, nil
}

func pageLimit(limit int) int {
	if limit <= 0 || limit > maxContentPage {
		return maxContentPage
	}
	return limit
}

// postRow 是读给插件的一条内容。
type postRow struct {
	ID          int64      `bun:"id" json:"id"`
	Type        string     `bun:"type" json:"type"`
	Title       string     `bun:"title" json:"title"`
	Slug        string     `bun:"slug" json:"slug"`
	Path        string     `bun:"-" json:"path"`
	Status      string     `bun:"status" json:"status"`
	Visibility  string     `bun:"visibility" json:"visibility"`
	AuthorID    int64      `bun:"author_id" json:"authorId"`
	Excerpt     string     `bun:"excerpt" json:"excerpt"`
	PublishedAt *time.Time `bun:"published_at" json:"publishedAt,omitempty"`
	UpdatedAt   time.Time  `bun:"updated_at" json:"updatedAt"`
	// 以下三项只在读单条时给
	Content string `bun:"content" json:"content,omitempty"`
	Raw     string `bun:"raw" json:"raw,omitempty"`
	RawType string `bun:"raw_type" json:"rawType,omitempty"`
}

const postSummaryColumns = `id, type, title, slug, status, visibility, author_id, excerpt, published_at, updated_at`

func hostPostsList(ctx context.Context, m *Module, _ *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		Search string `json:"search"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeArgs("content.posts.list", args, &in); err != nil {
		return nil, err
	}
	db, err := m.readDB()
	if err != nil {
		return nil, err
	}
	var rows []postRow
	q := db.NewSelect().TableExpr("posts").ColumnExpr(postSummaryColumns).Where("status <> 'trashed'")
	switch in.Type {
	case "":
	case string(content.TypePost), string(content.TypePage):
		q = q.Where("type = ?", in.Type)
	default:
		return nil, content.ErrInvalidType
	}
	switch in.Status {
	case "", "published":
		q = q.Where("status = 'published'")
	case "draft", "scheduled":
		q = q.Where("status = ?", in.Status)
	case "any":
	default:
		return nil, fmt.Errorf("status 只能是 published、draft、scheduled 或 any，实际 %q", in.Status)
	}
	if s := strings.TrimSpace(in.Search); s != "" {
		q = q.Where("title ILIKE ? ESCAPE '\\'", "%"+escapeLike(s)+"%")
	}
	total, err := q.OrderExpr("COALESCE(published_at, updated_at) DESC, id DESC").
		Limit(pageLimit(in.Limit)).Offset(max(in.Offset, 0)).ScanAndCount(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("读取内容列表: %w", err)
	}
	for i := range rows {
		rows[i].Path = hooks.PostPath(rows[i].Type, rows[i].Slug)
	}
	if rows == nil {
		rows = []postRow{}
	}
	return listResult{Items: rows, Total: total}, nil
}

func hostPostsGet(ctx context.Context, m *Module, _ *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
		Slug string `json:"slug"`
	}
	if err := decodeArgs("content.posts.get", args, &in); err != nil {
		return nil, err
	}
	db, err := m.readDB()
	if err != nil {
		return nil, err
	}
	row := new(postRow)
	q := db.NewSelect().TableExpr("posts").ColumnExpr(postSummaryColumns + ", content, raw, raw_type").
		Where("status <> 'trashed'")
	if in.ID > 0 {
		q = q.Where("id = ?", in.ID)
	} else {
		typ := in.Type
		if typ == "" {
			typ = string(content.TypePost)
		}
		q = q.Where("type = ? AND slug = ?", typ, in.Slug)
	}
	if err := q.Limit(1).Scan(ctx, row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("内容不存在")
		}
		return nil, fmt.Errorf("读取内容: %w", err)
	}
	row.Path = hooks.PostPath(row.Type, row.Slug)
	return row, nil
}

// commentRow 是读给插件的一条评论，形态同 comment.created 的数据。
type commentRow struct {
	ID          int64     `bun:"id"`
	ParentID    *int64    `bun:"parent_id"`
	PostID      int64     `bun:"post_id"`
	PostType    string    `bun:"post_type"`
	PostTitle   string    `bun:"post_title"`
	PostSlug    string    `bun:"post_slug"`
	UserID      *int64    `bun:"user_id"`
	AuthorName  string    `bun:"author_name"`
	AuthorEmail string    `bun:"author_email"`
	AuthorURL   string    `bun:"author_url"`
	Content     string    `bun:"content"`
	Status      string    `bun:"status"`
	IP          string    `bun:"ip"`
	UserAgent   string    `bun:"user_agent"`
	CreatedAt   time.Time `bun:"created_at"`
}

func (r *commentRow) hook() hooks.Comment {
	return hooks.Comment{
		ID: r.ID, ParentID: r.ParentID,
		Post:    hooks.PostRef{ID: r.PostID, Type: r.PostType, Title: r.PostTitle, Path: hooks.PostPath(r.PostType, r.PostSlug)},
		Author:  hooks.CommentAuthor{Name: r.AuthorName, Email: r.AuthorEmail, URL: r.AuthorURL, UserID: r.UserID},
		Content: r.Content, Status: r.Status, IP: r.IP, UserAgent: r.UserAgent, CreatedAt: r.CreatedAt,
	}
}

func commentQuery(db *bun.DB) *bun.SelectQuery {
	return db.NewSelect().TableExpr("comments AS c").Join("JOIN posts AS p ON p.id = c.post_id").ColumnExpr(
		"c.id, c.parent_id, c.post_id, p.type AS post_type, p.title AS post_title, p.slug AS post_slug, c.user_id, " +
			"c.author_name, c.author_email, c.author_url, c.content, c.status, c.ip, c.user_agent, c.created_at")
}

func hostCommentsList(ctx context.Context, m *Module, _ *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		PostID int64  `json:"postId"`
		Status string `json:"status"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeArgs("content.comments.list", args, &in); err != nil {
		return nil, err
	}
	db, err := m.readDB()
	if err != nil {
		return nil, err
	}
	var rows []commentRow
	q := commentQuery(db)
	if in.PostID > 0 {
		q = q.Where("c.post_id = ?", in.PostID)
	}
	switch in.Status {
	case "":
	case string(comment.StatusApproved), string(comment.StatusPending), string(comment.StatusSpam):
		q = q.Where("c.status = ?", in.Status)
	default:
		return nil, fmt.Errorf("status 只能是 approved、pending 或 spam，实际 %q", in.Status)
	}
	total, err := q.OrderExpr("c.id DESC").Limit(pageLimit(in.Limit)).Offset(max(in.Offset, 0)).ScanAndCount(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("读取评论列表: %w", err)
	}
	items := make([]hooks.Comment, 0, len(rows))
	for i := range rows {
		items = append(items, rows[i].hook())
	}
	return listResult{Items: items, Total: total}, nil
}

func hostCommentsGet(ctx context.Context, m *Module, _ *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := decodeArgs("content.comments.get", args, &in); err != nil {
		return nil, err
	}
	db, err := m.readDB()
	if err != nil {
		return nil, err
	}
	row := new(commentRow)
	if err := commentQuery(db).Where("c.id = ?", in.ID).Limit(1).Scan(ctx, row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("评论不存在")
		}
		return nil, fmt.Errorf("读取评论: %w", err)
	}
	return row.hook(), nil
}

func hostUsersGet(ctx context.Context, m *Module, _ *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := decodeArgs("content.users.get", args, &in); err != nil {
		return nil, err
	}
	db, err := m.readDB()
	if err != nil {
		return nil, err
	}
	// 只有公开资料：邮箱、登录信息一概不给
	var out struct {
		ID          int64  `bun:"id" json:"id"`
		Username    string `bun:"username" json:"username"`
		DisplayName string `bun:"display_name" json:"displayName"`
		AvatarURL   string `bun:"avatar_url" json:"avatarUrl"`
		Bio         string `bun:"bio" json:"bio"`
	}
	err = db.NewSelect().TableExpr("users").ColumnExpr("id, username, display_name, avatar_url, bio").
		Where("id = ?", in.ID).Limit(1).Scan(ctx, &out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("用户不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("读取用户: %w", err)
	}
	return out, nil
}

func hostTermsList(ctx context.Context, m *Module, _ *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		Kind string `json:"kind"`
	}
	if err := decodeArgs("content.terms.list", args, &in); err != nil {
		return nil, err
	}
	table := map[string]string{"category": "categories", "tag": "tags"}[in.Kind]
	if table == "" {
		return nil, fmt.Errorf("kind 只能是 category 或 tag，实际 %q", in.Kind)
	}
	db, err := m.readDB()
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID   int64  `bun:"id" json:"id"`
		Name string `bun:"name" json:"name"`
		Slug string `bun:"slug" json:"slug"`
	}
	if err := db.NewSelect().TableExpr(table).ColumnExpr("id, name, slug").OrderExpr("name").
		Limit(1000).Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("读取%s: %w", in.Kind, err)
	}
	return listResult{Items: rows}, nil
}

// ---------- 写 ----------

// writeArgs 是写内容的参数。
type writeArgs struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
	content.WriteParams
	// Publish 为真时新建即发布（还要 posts:publish / pages:publish）。
	Publish bool `json:"publish"`
}

// postPerms 按内容类型挑出对应的权限串：文章用 posts:*，页面用 pages:*。
func postPerms(typ content.Type) (write, writeAny, publish, deleteAny perm.Permission, err error) {
	switch typ {
	case content.TypePost:
		return perm.PostsWrite, perm.PostsWriteAny, perm.PostsPublish, perm.PostsDeleteAny, nil
	case content.TypePage:
		return perm.PagesWrite, perm.PagesWriteAny, perm.PagesPublish, perm.PagesDeleteAny, nil
	}
	return "", "", "", "", content.ErrInvalidType
}

func (m *Module) writer() (*content.Writer, error) {
	if m.content == nil || m.content.Writer() == nil {
		return nil, errors.New("内容模块不可用")
	}
	return m.content.Writer(), nil
}

// siteOwner 返回最早的超级管理员：插件新建的内容记在站长名下。
func (m *Module) siteOwner(ctx context.Context) (int64, error) {
	db, err := m.readDB()
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.NewRaw(`SELECT u.id FROM users u
		JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		WHERE r.name = ? ORDER BY u.id LIMIT 1`, perm.RoleSuperAdmin).Scan(ctx, &id)
	if err != nil {
		return 0, fmt.Errorf("找不到站长账号: %w", err)
	}
	return id, nil
}

func hostPostsCreate(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in writeArgs
	if err := decodeArgs("content.posts.create", args, &in); err != nil {
		return nil, err
	}
	typ := content.Type(in.Type)
	if typ == "" {
		typ = content.TypePost
	}
	write, _, publish, _, err := postPerms(typ)
	if err != nil {
		return nil, err
	}
	if permErr := requireWrite(loaded, write); permErr != nil {
		return nil, permErr
	}
	if in.Publish {
		if permErr := requireWrite(loaded, publish); permErr != nil {
			return nil, permErr
		}
	}
	w, err := m.writer()
	if err != nil {
		return nil, err
	}
	owner, err := m.siteOwner(ctx)
	if err != nil {
		return nil, err
	}
	post, err := w.Create(ctx, typ, owner, &in.WriteParams, in.Publish)
	if err != nil {
		return nil, err
	}
	return content.HookPost(post), nil
}

func hostPostsUpdate(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in writeArgs
	if err := decodeArgs("content.posts.update", args, &in); err != nil {
		return nil, err
	}
	typ := content.Type(in.Type)
	if typ == "" {
		typ = content.TypePost
	}
	_, writeAny, _, _, err := postPerms(typ)
	if err != nil {
		return nil, err
	}
	// 插件没有「自己的」内容：改谁写的都算改别人的
	if permErr := requireWrite(loaded, writeAny); permErr != nil {
		return nil, permErr
	}
	w, err := m.writer()
	if err != nil {
		return nil, err
	}
	owner, err := m.siteOwner(ctx)
	if err != nil {
		return nil, err
	}
	post, err := w.Update(ctx, typ, in.ID, &in.WriteParams, owner)
	if err != nil {
		return nil, err
	}
	return content.HookPost(post), nil
}

func hostPostsTrash(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in writeArgs
	if err := decodeArgs("content.posts.trash", args, &in); err != nil {
		return nil, err
	}
	typ := content.Type(in.Type)
	if typ == "" {
		typ = content.TypePost
	}
	_, _, _, deleteAny, err := postPerms(typ)
	if err != nil {
		return nil, err
	}
	if permErr := requireWrite(loaded, deleteAny); permErr != nil {
		return nil, permErr
	}
	w, err := m.writer()
	if err != nil {
		return nil, err
	}
	return nil, w.Trash(ctx, typ, in.ID)
}

func (m *Module) commentsModule() (*comment.Module, error) {
	if m.comments == nil {
		return nil, errors.New("评论模块不可用")
	}
	return m.comments, nil
}

func hostCommentsModerate(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := decodeArgs("content.comments.moderate", args, &in); err != nil {
		return nil, err
	}
	if err := requireWrite(loaded, perm.CommentsManageAny); err != nil {
		return nil, err
	}
	comments, err := m.commentsModule()
	if err != nil {
		return nil, err
	}
	return nil, comments.Moderate(ctx, in.ID, comment.Status(in.Status))
}

func hostCommentsDelete(ctx context.Context, m *Module, loaded *Loaded, args json.RawMessage) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := decodeArgs("content.comments.delete", args, &in); err != nil {
		return nil, err
	}
	if err := requireWrite(loaded, perm.CommentsManageAny); err != nil {
		return nil, err
	}
	comments, err := m.commentsModule()
	if err != nil {
		return nil, err
	}
	return nil, comments.Delete(ctx, in.ID)
}
