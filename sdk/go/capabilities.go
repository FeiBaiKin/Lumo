package lumo

import (
	"encoding/base64"
	"encoding/json"
	"time"
	"unicode/utf8"
)

// 需要在 plugin.yaml 的 capabilities 里声明、由站长启用时确认的宿主能力。

// ---------- 站点内容（capabilities.content） ----------

// Content 是站点内容的读写入口。读要 content.read；写按 content.write 里声明的权限串判定：
// 新建文章 posts:write（直接发布再加 posts:publish），改文章 posts:write_any，移入回收站 posts:delete_any，
// 页面同理换成 pages:*；审核与删除评论 comments:manage_any。插件新建的内容记在站长名下。
var Content contentAPI

type contentAPI struct{}

// PostItem 是读出来的一条文章或页面。Content、Raw、RawType 只在读单条时有。
type PostItem struct {
	ID          int64      `json:"id"`
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Path        string     `json:"path"`
	Status      string     `json:"status"`
	Visibility  string     `json:"visibility"`
	AuthorID    int64      `json:"authorId"`
	Excerpt     string     `json:"excerpt"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	Content     string     `json:"content,omitempty"`
	Raw         string     `json:"raw,omitempty"`
	RawType     string     `json:"rawType,omitempty"`
}

// PostQuery 是列出内容的条件。
type PostQuery struct {
	// Type 是 post 或 page，留空两种都要。
	Type string `json:"type,omitempty"`
	// Status 是 published（缺省）、draft、scheduled 或 any；回收站里的一律不给。
	Status string `json:"status,omitempty"`
	// Search 在标题里模糊查找。
	Search string `json:"search,omitempty"`
	// Limit 最大 100。
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// Posts 列出文章与页面，按发布时间倒序，返回这一页与总数。
func (contentAPI) Posts(q PostQuery) ([]PostItem, int, error) {
	var out struct {
		Items []PostItem `json:"items"`
		Total int        `json:"total"`
	}
	err := call("content.posts.list", q, &out)
	return out.Items, out.Total, err
}

// Post 按 ID 读一条文章或页面，含正文。
func (contentAPI) Post(id int64) (*PostItem, error) {
	out := new(PostItem)
	return out, call("content.posts.get", map[string]int64{"id": id}, out)
}

// PostBySlug 按类型与 slug 读一条，typ 为 post 或 page。
func (contentAPI) PostBySlug(typ, slug string) (*PostItem, error) {
	out := new(PostItem)
	return out, call("content.posts.get", map[string]string{"type": typ, "slug": slug}, out)
}

// PostInput 是新建或修改内容的字段。
type PostInput struct {
	// Type 是 post（缺省）或 page。
	Type  string `json:"type,omitempty"`
	Title string `json:"title"`
	// Slug 留空时新建由标题生成、修改保留原值。
	Slug string `json:"slug,omitempty"`
	// RawType 是 html（缺省）或 markdown。
	RawType     string  `json:"rawType,omitempty"`
	Raw         string  `json:"raw"`
	Excerpt     string  `json:"excerpt,omitempty"`
	CategoryIDs []int64 `json:"categoryIds,omitempty"`
	TagIDs      []int64 `json:"tagIds,omitempty"`
	// Publish 为真时新建即发布。
	Publish bool `json:"publish,omitempty"`
}

// CreatePost 新建一条文章或页面。正文按普通作者的规则净化。
func (contentAPI) CreatePost(in PostInput) (*Post, error) {
	out := new(Post)
	return out, call("content.posts.create", in, out)
}

// UpdatePost 改写一条内容的标题与正文，不改变它的发布状态。
func (contentAPI) UpdatePost(id int64, in PostInput) (*Post, error) {
	args := struct {
		ID int64 `json:"id"`
		PostInput
	}{id, in}
	out := new(Post)
	return out, call("content.posts.update", args, out)
}

// TrashPost 把一条内容移入回收站，typ 为 post 或 page。
func (contentAPI) TrashPost(typ string, id int64) error {
	return call("content.posts.trash", map[string]any{"type": typ, "id": id}, nil)
}

// CommentQuery 是列出评论的条件。
type CommentQuery struct {
	PostID int64 `json:"postId,omitempty"`
	// Status 是 approved、pending 或 spam，留空全要。
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// Comments 列出评论，新的在前。
func (contentAPI) Comments(q CommentQuery) ([]Comment, int, error) {
	var out struct {
		Items []Comment `json:"items"`
		Total int       `json:"total"`
	}
	err := call("content.comments.list", q, &out)
	return out.Items, out.Total, err
}

// Comment 按 ID 读一条评论。
func (contentAPI) Comment(id int64) (*Comment, error) {
	out := new(Comment)
	return out, call("content.comments.get", map[string]int64{"id": id}, out)
}

// ModerateComment 改评论状态：approved、pending 或 spam。
func (contentAPI) ModerateComment(id int64, status string) error {
	return call("content.comments.moderate", map[string]any{"id": id, "status": status}, nil)
}

// DeleteComment 删一条评论。
func (contentAPI) DeleteComment(id int64) error {
	return call("content.comments.delete", map[string]int64{"id": id}, nil)
}

// UserProfile 是用户的公开资料。
type UserProfile struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
	Bio         string `json:"bio"`
}

// User 读一个用户的公开资料。
func (contentAPI) User(id int64) (*UserProfile, error) {
	out := new(UserProfile)
	return out, call("content.users.get", map[string]int64{"id": id}, out)
}

// Term 是一个分类或标签。
type Term struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func terms(kind string) ([]Term, error) {
	var out struct {
		Items []Term `json:"items"`
	}
	err := call("content.terms.list", map[string]string{"kind": kind}, &out)
	return out.Items, err
}

// Categories 列出全部分类。
func (contentAPI) Categories() ([]Term, error) { return terms("category") }

// Tags 列出全部标签。
func (contentAPI) Tags() ([]Term, error) { return terms("tag") }

// ---------- 外部网络（capabilities.http） ----------

// FetchRequest 是一次对外请求。只能访问清单里声明的域名，内网与本机地址一律拒绝。
type FetchRequest struct {
	// Method 缺省 GET。
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
	// Timeout 最长 10 秒；为零时用 10 秒。
	Timeout time.Duration
}

// FetchResponse 是对外请求的响应。
type FetchResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
	// Truncated 为真表示响应体超过 4 MiB，后面的被截掉了。
	Truncated bool
}

// JSON 把响应体解到 v 里。
func (r *FetchResponse) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// Fetch 发出一次对外请求。重定向最多 3 次，每一跳都要落在声明的域名里。
func Fetch(req FetchRequest) (*FetchResponse, error) {
	args := map[string]any{"method": req.Method, "url": req.URL, "headers": req.Headers}
	if utf8.Valid(req.Body) {
		args["body"] = string(req.Body)
	} else {
		args["bodyBase64"] = base64.StdEncoding.EncodeToString(req.Body)
	}
	if req.Timeout > 0 {
		args["timeout"] = req.Timeout.Milliseconds()
	}
	var out struct {
		Status     int               `json:"status"`
		Headers    map[string]string `json:"headers"`
		Body       string            `json:"body"`
		BodyBase64 string            `json:"bodyBase64"`
		Truncated  bool              `json:"truncated"`
	}
	if err := call("http.fetch", args, &out); err != nil {
		return nil, err
	}
	resp := &FetchResponse{Status: out.Status, Headers: out.Headers, Body: []byte(out.Body), Truncated: out.Truncated}
	if out.BodyBase64 != "" {
		body, err := base64.StdEncoding.DecodeString(out.BodyBase64)
		if err != nil {
			return nil, err
		}
		resp.Body = body
	}
	return resp, nil
}

// ---------- 邮件（capabilities.mail） ----------

// Mail 是一封邮件。
type Mail struct {
	// To 是收件人，1 到 10 个。
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	// Text 是纯文本正文，必填。
	Text string `json:"text"`
	// HTML 是可选的 HTML 正文。
	HTML string `json:"html,omitempty"`
}

// SendMail 经站点配置的 SMTP 发一封邮件（进发送队列，失败会重试）。每个插件每小时最多 30 封。
func SendMail(m Mail) error { return call("mail.send", m, nil) }

// ---------- 定时任务（capabilities.cron） ----------

// OnCron 登记一个定时任务，名字对应 plugin.yaml 里 spec.cron 的 name，间隔也写在那里。
// 多实例部署时每个周期只有一个实例执行；单次时限 60 秒。
func OnCron(name string, fn func(ctx *Context) error) {
	register("cron", name, func(ctx *Context, _ json.RawMessage) (any, error) {
		return nil, fn(ctx)
	})
}
