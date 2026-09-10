package comment

import (
	"context"
	"time"
)

// ConfigFunc 返回评论设置；由模块装配时注入设置服务，测试可传 nil 用缺省值。
type ConfigFunc func(ctx context.Context) Settings

// defaultSettings 是未装配设置模块时的取值，与分组缺省值保持一致。
var defaultSettings = Settings{
	Enabled:         true,
	AllowAnonymous:  true,
	RequireEmail:    true,
	RequireApproval: true,
	MaxLength:       2000,
	IntervalSeconds: 30,
	MaxLinks:        3,
}

// NotifyEvent 描述一次需要通知的评论事件。
type NotifyEvent struct {
	Comment *Comment
	Post    *PostRef
	// Settings 是判定时的评论设置，避免通知侧再读一次。
	Settings Settings
	// AutoApproved 为真表示该评论已直接展示，通知里不必再提「待审核」。
	AutoApproved bool
	// Parent 是被回复的评论，仅在这是一条回复时有值。
	Parent *Comment
}

// Notifier 在评论创建后决定要不要发通知。
//
// 实现必须立即返回：它跑在请求路径上，而发信要走后台队列。
type Notifier interface {
	CommentCreated(ctx context.Context, event *NotifyEvent)
}

// nowFunc 供测试替换时钟。
var nowFunc = time.Now
