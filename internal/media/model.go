package media

import (
	"time"

	"github.com/uptrace/bun"
)

// Kind 是附件的粗分类，由 MIME 推导，供 Console 侧筛选。
type Kind string

// 附件分类。
const (
	KindImage    Kind = "image"
	KindVideo    Kind = "video"
	KindAudio    Kind = "audio"
	KindDocument Kind = "document"
	KindOther    Kind = "other"
)

// Thumbnail 是一档缩略图。
type Thumbnail struct {
	Name   string `json:"name" doc:"档位名，如 thumb / medium / large"`
	Key    string `json:"key" doc:"存储内的对象键"`
	URL    string `json:"url" doc:"公开访问地址"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int64  `json:"size" doc:"字节数"`
}

// Media 是一条附件记录。
type Media struct {
	bun.BaseModel `bun:"table:media,alias:m"`

	ID           int64       `bun:"id,pk,autoincrement" json:"id"`
	Filename     string      `bun:"filename,notnull"    json:"filename" doc:"存储用的随机化文件名"`
	OriginalName string      `bun:"original_name"       json:"originalName" doc:"上传时的原始文件名，仅作展示"`
	MIME         string      `bun:"mime,notnull"        json:"mime"`
	Kind         Kind        `bun:"kind,notnull"        json:"kind" enum:"image,video,audio,document,other"`
	Size         int64       `bun:"size"                json:"size" doc:"字节数"`
	Width        int         `bun:"width"               json:"width" doc:"图片宽度；非图片为 0"`
	Height       int         `bun:"height"              json:"height" doc:"图片高度；非图片为 0"`
	Driver       string      `bun:"driver,notnull"      json:"driver" doc:"写入时使用的存储驱动"`
	StorageKey   string      `bun:"storage_key,notnull" json:"storageKey" doc:"存储内的对象键"`
	URL          string      `bun:"url"                 json:"url" doc:"公开访问地址"`
	Thumbnails   []Thumbnail `bun:"thumbnails,type:jsonb" json:"thumbnails" doc:"按最长边生成的多档 WebP 缩略图"`
	Checksum     string      `bun:"checksum"            json:"checksum" doc:"原文件的 SHA-256"`
	UploaderID   int64       `bun:"uploader_id,notnull" json:"uploaderId"`
	Alt          string      `bun:"alt"                 json:"alt" doc:"无障碍替代文本"`
	Title        string      `bun:"title"               json:"title" doc:"标题"`
	CreatedAt    time.Time   `bun:"created_at,nullzero" json:"createdAt"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero" json:"updatedAt"`

	// Uploader 由存储层按需填充，不是数据库列。
	Uploader *UploaderView `bun:"-" json:"uploader,omitempty"`
}

// UploaderView 是嵌入附件响应的上传者摘要，只含可公开的字段。
type UploaderView struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

// Keys 返回该附件占用的全部对象键：原文件加各档缩略图。
//
// 删除附件时按此清单逐个删除，避免缩略图变成孤儿文件。
func (m *Media) Keys() []string {
	keys := make([]string, 0, 1+len(m.Thumbnails))
	keys = append(keys, m.StorageKey)
	for _, t := range m.Thumbnails {
		keys = append(keys, t.Key)
	}
	return keys
}
