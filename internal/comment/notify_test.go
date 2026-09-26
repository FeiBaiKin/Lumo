package comment

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/FeiBaiKin/lumo/internal/testdb"
)

// 新评论通知的收件人：填了地址发给它，留空发给内容作者，作者评论自己的内容不通知。
func TestNotifyRecipient(t *testing.T) {
	var asked []int64
	n := &notifier{
		logger: slog.New(slog.DiscardHandler),
		authorEmail: func(_ context.Context, id int64) (string, error) {
			asked = append(asked, id)
			if id == 9 {
				return "", errors.New("库连不上")
			}
			return "author@example.com", nil
		},
	}
	author := int64(7)
	other := int64(8)
	event := func(authorID int64, commenter *int64) *NotifyEvent {
		return &NotifyEvent{Post: &PostRef{ID: 1, AuthorID: authorID}, Comment: &Comment{UserID: commenter}}
	}
	cases := []struct {
		name     string
		notifyTo string
		event    *NotifyEvent
		want     string
	}{
		{"填了地址", " owner@example.com ", event(7, nil), "owner@example.com"},
		{"留空发给作者", "", event(7, nil), "author@example.com"},
		{"别的登录用户评论也发给作者", "", event(7, &other), "author@example.com"},
		{"作者评论自己的内容", "", event(7, &author), ""},
		{"没有作者", "", event(0, nil), ""},
		{"查作者出错", "", event(9, nil), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := n.recipient(context.Background(), Settings{NotifyTo: c.notifyTo}, c.event); got != c.want {
				t.Fatalf("收件人该是 %q，得到 %q", c.want, got)
			}
		})
	}
	if len(asked) != 3 {
		t.Fatalf("只有留空且不是作者本人时才该查作者，查了 %v", asked)
	}

	// 没有库时留空就不发
	if got := (&notifier{}).recipient(context.Background(), Settings{}, event(7, nil)); got != "" {
		t.Fatalf("没有库时不该有收件人，得到 %q", got)
	}
}

// 邮件里的后台链接要带站点地址；没配地址时不给相对链接。
func TestNotifyConsoleHint(t *testing.T) {
	withURL := &notifier{siteURL: func(context.Context) string { return "https://example.com" }}
	if got := withURL.consoleHint(context.Background()); got != "在后台查看：https://example.com/console/comments" {
		t.Fatalf("配了站点地址时该给完整链接，得到 %q", got)
	}
	for _, n := range []*notifier{{}, {siteURL: func(context.Context) string { return "" }}} {
		if got := n.consoleHint(context.Background()); got != "到后台的「评论」里查看。" {
			t.Fatalf("没配站点地址时不该给链接，得到 %q", got)
		}
	}
}

func TestStoreAuthorEmail(t *testing.T) {
	db := testdb.OpenCore(t)
	ctx := context.Background()
	var id int64
	err := db.DB.NewRaw("INSERT INTO users (username, email, password_hash) VALUES ('writer', 'writer@example.com', 'x') RETURNING id").
		Scan(ctx, &id)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db.DB)

	if got, err := store.AuthorEmail(ctx, id); err != nil || got != "writer@example.com" {
		t.Fatalf("该取到作者邮箱，得到 %q, %v", got, err)
	}
	if got, err := store.AuthorEmail(ctx, id+100); err != nil || got != "" {
		t.Fatalf("不存在的用户该返回空串，得到 %q, %v", got, err)
	}
	if _, err := db.DB.NewRaw("UPDATE users SET disabled = true WHERE id = ?", id).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := store.AuthorEmail(ctx, id); err != nil || got != "" {
		t.Fatalf("停用的账号不该收通知，得到 %q, %v", got, err)
	}
}
