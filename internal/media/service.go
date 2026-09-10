package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FeiBaiKin/lumo/internal/settings"
)

// UploadsDirName 是本地驱动在工作目录下的子目录名（与 workdir.Subdirs 一致）。
const UploadsDirName = "uploads"

// UploadsURLPrefix 是本地驱动的对外访问前缀，由核心路由挂载静态文件服务。
const UploadsURLPrefix = "/" + UploadsDirName

// maxImageBytes 是允许载入内存做缩略图的原图大小上限。
//
// 超过此值的图片仍然照常存下，只是不生成缩略图：缩略图是锦上添花，
// 不值得为它把一整个几十兆的文件读进内存。
const maxImageBytes = 32 << 20

// maxDisplayNameLength 是原始文件名的保留长度，超出部分截断。
const maxDisplayNameLength = 200

// ErrTooLarge 表示上传内容超过服务端允许的大小。
var ErrTooLarge = errors.New("文件超过允许的大小")

// Service 编排上传：类型判定、写存储、生成缩略图、落库。
type Service struct {
	store     *Store
	settings  *settings.Service
	images    ImageProcessor
	dataDir   string
	logger    *slog.Logger
	specs     []ThumbnailSpec
	maxUpload int64

	// 存储实例按设置缓存：每次上传都重建 S3 客户端会白白丢掉连接池。
	mu       sync.Mutex
	cachedAt StorageSettings
	cached   Storage
}

// ServiceOptions 是构造 Service 的依赖。
type ServiceOptions struct {
	Store *Store
	// Settings 可为 nil：此时一律使用本地存储的默认配置。
	Settings *settings.Service
	// Images 可为 nil：缺省用纯 Go 实现。
	Images ImageProcessor
	// DataDir 是工作目录，本地存储写在它的 uploads 子目录下。
	DataDir string
	Logger  *slog.Logger
	// MaxUpload 是单个文件的字节上限，0 表示不额外限制（仍受路由层请求体上限约束）。
	MaxUpload int64
}

// NewService 构造 Service。
func NewService(opts ServiceOptions) *Service {
	images := opts.Images
	if images == nil {
		images = NewImageProcessor()
	}
	return &Service{
		store:     opts.Store,
		settings:  opts.Settings,
		images:    images,
		dataDir:   opts.DataDir,
		logger:    opts.Logger,
		specs:     DefaultThumbnails,
		maxUpload: opts.MaxUpload,
	}
}

// UploadParams 描述一次上传。
type UploadParams struct {
	// OriginalName 是客户端给出的文件名，只用于取扩展名与展示，绝不参与路径拼接。
	OriginalName string
	// Size 是内容字节数的客户端声明值：用于超限预判与 S3 分片决策，落库以实际读到的字节数为准。
	Size    int64
	Content io.Reader
	Alt     string
	Title   string
	// UploaderID 是上传者的用户 ID。
	UploaderID int64
}

// Upload 保存一个上传的文件并返回落库后的记录。
func (s *Service) Upload(ctx context.Context, params *UploadParams) (*Media, error) {
	if s.maxUpload > 0 && params.Size > s.maxUpload {
		return nil, fmt.Errorf("%w：上限 %d 字节", ErrTooLarge, s.maxUpload)
	}

	head, body, err := peek(params.Content, sniffLen)
	if err != nil {
		return nil, err
	}
	ft, err := detectType(params.OriginalName, head)
	if err != nil {
		return nil, err
	}

	storage, err := s.Storage(ctx)
	if err != nil {
		return nil, err
	}
	key, name, err := newObjectKey(time.Now(), ft.Ext)
	if err != nil {
		return nil, err
	}

	record := &Media{
		Filename:     name,
		OriginalName: sanitizeDisplayName(params.OriginalName),
		MIME:         ft.MIME,
		Kind:         ft.Kind,
		Driver:       storage.Driver(),
		StorageKey:   key,
		URL:          storage.URL(key),
		Thumbnails:   []Thumbnail{},
		UploaderID:   params.UploaderID,
		Alt:          params.Alt,
		Title:        params.Title,
	}

	// 写坏一半就失败时要把已经落地的对象清掉，否则存储里会留下永远无人引用的孤儿文件。
	written := make([]string, 0, 1+len(s.specs))
	cleanup := func() {
		for _, k := range written {
			// 用 WithoutCancel：请求已经出错（可能连 ctx 都取消了），清理仍必须执行完。
			if delErr := storage.Delete(context.WithoutCancel(ctx), k); delErr != nil && s.logger != nil {
				s.logger.Warn("回滚上传时删除对象失败", slog.String("key", k), slog.Any("error", delErr))
			}
		}
	}

	if ft.Image && params.Size <= maxImageBytes {
		err = s.putImage(ctx, storage, record, body, &written)
	} else {
		err = s.putStream(ctx, storage, record, body, params.Size, &written)
	}
	if err != nil {
		cleanup()
		return nil, err
	}

	if err := s.store.Create(ctx, record); err != nil {
		cleanup()
		return nil, err
	}

	// 回填上传者，让创建接口的响应与列表接口结构一致。
	items := []Media{*record}
	if err := s.store.attach(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// putImage 把图片读进内存，存原图并生成各档缩略图。
func (s *Service) putImage(ctx context.Context, storage Storage, record *Media, body io.Reader, written *[]string) error {
	data, err := io.ReadAll(io.LimitReader(body, maxImageBytes+1))
	if err != nil {
		return fmt.Errorf("读取上传内容: %w", err)
	}
	if int64(len(data)) > maxImageBytes {
		return fmt.Errorf("%w：图片上限 %d 字节", ErrTooLarge, int64(maxImageBytes))
	}

	sum := sha256.Sum256(data)
	record.Size = int64(len(data))
	record.Checksum = hex.EncodeToString(sum[:])

	if putErr := storage.Put(ctx, record.StorageKey, bytes.NewReader(data), record.Size, record.MIME); putErr != nil {
		return putErr
	}
	*written = append(*written, record.StorageKey)

	info, variants, err := s.images.Process(data, s.specs)
	if err != nil {
		// 缩略图失败不该让整次上传失败：原图已经存好，前台仍可用，退化为无缩略图即可。
		if s.logger != nil {
			s.logger.Warn("生成缩略图失败，仅保留原图",
				slog.String("file", record.OriginalName), slog.Any("error", err))
		}
		return nil
	}
	record.Width, record.Height = info.Width, info.Height

	for _, v := range variants {
		key := variantKey(record.StorageKey, v.Name, v.Ext)
		if err := storage.Put(ctx, key, bytes.NewReader(v.Data), int64(len(v.Data)), v.MIME); err != nil {
			return err
		}
		*written = append(*written, key)
		record.Thumbnails = append(record.Thumbnails, Thumbnail{
			Name: v.Name, Key: key, URL: storage.URL(key),
			Width: v.Width, Height: v.Height, Size: int64(len(v.Data)),
		})
	}
	return nil
}

// putStream 边算校验和边把内容写入存储，不在内存里留副本。
func (s *Service) putStream(ctx context.Context, storage Storage, record *Media, body io.Reader, size int64, written *[]string) error {
	digest := sha256.New()
	counter := &countingReader{r: io.TeeReader(body, digest)}
	if err := storage.Put(ctx, record.StorageKey, counter, size, record.MIME); err != nil {
		return err
	}
	*written = append(*written, record.StorageKey)
	record.Size = counter.n
	record.Checksum = hex.EncodeToString(digest.Sum(nil))
	return nil
}

// countingReader 统计实际读过的字节数：客户端声明的大小不可信，落库要用真实值。
type countingReader struct {
	r io.Reader
	n int64
}

// Read 实现 io.Reader。
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Delete 删除附件记录并清理其占用的全部对象。
//
// 先删库再删文件：反过来一旦删完文件后落库失败，就会留下指向空文件的记录，
// 那比留下几个孤儿文件更糟——前台会拿到 404 图片。
func (s *Service) Delete(ctx context.Context, m *Media) error {
	if err := s.store.Delete(ctx, m.ID); err != nil {
		return err
	}
	storage, err := s.storageFor(ctx, m.Driver)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("附件记录已删除，但无法访问其存储，文件未清理",
				slog.String("driver", m.Driver), slog.Int64("id", m.ID), slog.Any("error", err))
		}
		return nil
	}
	for _, key := range m.Keys() {
		if err := storage.Delete(ctx, key); err != nil && s.logger != nil {
			s.logger.Warn("删除附件文件失败", slog.String("key", key), slog.Any("error", err))
		}
	}
	return nil
}

// Storage 返回当前设置对应的存储实例。
func (s *Service) Storage(ctx context.Context) (Storage, error) {
	cfg := s.storageSettings(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.cachedAt == cfg {
		return s.cached, nil
	}
	built, err := s.build(&cfg)
	if err != nil {
		return nil, err
	}
	s.cachedAt, s.cached = cfg, built
	return built, nil
}

// storageFor 返回指定驱动的存储实例，供删除历史记录时使用。
//
// 站点换过存储后，老记录的文件仍在原来的驱动里，必须按记录上的驱动去删。
func (s *Service) storageFor(ctx context.Context, driver string) (Storage, error) {
	current, err := s.Storage(ctx)
	if err == nil && current.Driver() == driver {
		return current, nil
	}
	switch driver {
	case DriverLocal:
		return s.newLocal()
	case DriverS3:
		cfg := s.storageSettings(ctx)
		cfg.Driver = DriverS3
		return s.build(&cfg)
	default:
		return nil, fmt.Errorf("media: 未知的存储驱动 %q", driver)
	}
}

// storageSettings 读取 storage 分组的有效值；未装配设置模块或读取失败时退回本地存储。
func (s *Service) storageSettings(ctx context.Context) StorageSettings {
	local := StorageSettings{Driver: DriverLocal}
	if s.settings == nil {
		return local
	}
	var cfg StorageSettings
	if err := s.settings.Get(ctx, GroupStorage, &cfg); err != nil {
		if s.logger != nil {
			s.logger.Warn("读取存储设置失败，退回本地存储", slog.Any("error", err))
		}
		return local
	}
	if cfg.Driver == "" {
		cfg.Driver = DriverLocal
	}
	return cfg
}

// build 按设置构造存储实例。
func (s *Service) build(cfg *StorageSettings) (Storage, error) {
	if cfg.Driver != DriverS3 {
		return s.newLocal()
	}
	accessKey, secretKey, err := S3Credentials()
	if err != nil {
		return nil, err
	}
	return NewS3Storage(S3Config{
		Endpoint:  cfg.S3Endpoint,
		Bucket:    cfg.S3Bucket,
		Region:    cfg.S3Region,
		PathStyle: cfg.S3PathStyle,
		UseSSL:    cfg.S3UseSSL,
		PublicURL: cfg.S3PublicURL,
	}, accessKey, secretKey)
}

// newLocal 构造本地存储实例。
func (s *Service) newLocal() (Storage, error) {
	dir := s.dataDir
	if dir == "" {
		dir = "./data"
	}
	return NewLocalStorage(filepath.Join(dir, UploadsDirName), UploadsURLPrefix)
}

// peek 读出前 n 字节用于嗅探，并返回一个能重新读到完整内容的 Reader。
func peek(r io.Reader, n int) (head []byte, body io.Reader, err error) {
	buf := make([]byte, n)
	read, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, nil, fmt.Errorf("读取上传内容: %w", err)
	}
	head = buf[:read]
	// 能定位就退回开头，省掉一份缓冲；不能定位（如网络流）则把已读部分接回去。
	if seeker, ok := r.(io.Seeker); ok {
		if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr == nil {
			return head, r, nil
		}
	}
	return head, io.MultiReader(bytes.NewReader(head), r), nil
}

// sanitizeDisplayName 清洗用于展示的原始文件名。
//
// 只去掉目录部分与控制字符并截断长度：它永远不参与路径拼接（对象键是随机生成的），
// 但会出现在 Console 列表与接口响应里，不该把 ../ 或换行带进去。
func sanitizeDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > maxDisplayNameLength {
		name = string(runes[:maxDisplayNameLength])
	}
	if name == "" {
		return "未命名"
	}
	return name
}
