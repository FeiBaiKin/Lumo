package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// FilePrefix 是日志文件名的固定前缀，读取端据此识别日志文件。
const FilePrefix = "lumo-"

// FileSuffix 是日志文件的扩展名。
const FileSuffix = ".log"

// fileNamePattern 匹配 lumo-2026-09-21.log 与 lumo-2026-09-21.2.log。
var fileNamePattern = regexp.MustCompile(`^lumo-(\d{4}-\d{2}-\d{2})(?:\.(\d+))?\.log$`)

// RotateOptions 是滚动写入器的参数。
type RotateOptions struct {
	// Dir 是日志目录。
	Dir string
	// RetainDays 是保留天数，按文件名里的日期判断；小于 1 时用 defaultRetainDays。
	RetainDays int
	// MaxSizeMB 是单文件大小上限，超过则在同一天内滚到下一个序号；
	// 小于 1 时用 defaultMaxSizeMB。
	MaxSizeMB int
}

const (
	defaultRetainDays = 14
	defaultMaxSizeMB  = 100
)

// RotatingFile 是按天切割、按大小滚动的日志写入器。
//
// 自己实现而不是引第三方轮转库：需要的只是「按天切 + 超限换一个 + 删过期」这三件事，
// 而日志文件名的形态同时是读取端的契约（见 internal/logs），由本包定义最稳妥。
//
// 并发安全：slog 的 handler 会被多个请求并发调用，Write 必须串行化。
type RotatingFile struct {
	dir        string
	retainDays int
	maxSize    int64

	mu      sync.Mutex
	file    *os.File
	day     string // 当前文件所属日期，形如 2026-09-21
	seq     int    // 同一天内的序号，0 表示无后缀
	written int64
}

// NewRotatingFile 构造写入器并打开当天的文件。
func NewRotatingFile(opts RotateOptions) (*RotatingFile, error) {
	if opts.RetainDays < 1 {
		opts.RetainDays = defaultRetainDays
	}
	if opts.MaxSizeMB < 1 {
		opts.MaxSizeMB = defaultMaxSizeMB
	}
	if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建日志目录: %w", err)
	}
	w := &RotatingFile{
		dir:        opts.Dir,
		retainDays: opts.RetainDays,
		maxSize:    int64(opts.MaxSizeMB) << 20,
	}
	if err := w.openFor(time.Now()); err != nil {
		return nil, err
	}
	w.cleanup()
	return w, nil
}

// Write 实现 io.Writer。
//
// 写失败不向上报错到调用方以外的地方：日志写不进磁盘时，进程仍应继续服务，
// stdout 那一路也还在（见 New 的多路输出）。
func (w *RotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	if day := now.Format(time.DateOnly); day != w.day {
		// 跨天：换文件，并顺手清掉过期的。
		if err := w.rotateTo(now, 0); err != nil {
			return 0, err
		}
		go w.cleanup()
	} else if w.written+int64(len(p)) > w.maxSize {
		if err := w.rotateTo(now, w.seq+1); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.written += int64(n)
	return n, err
}

// Close 关闭当前文件。
func (w *RotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// openFor 打开给定时刻对应的文件，选出当天尚未写满的最大序号。
func (w *RotatingFile) openFor(t time.Time) error {
	day := t.Format(time.DateOnly)
	seq := 0
	for {
		path := filepath.Join(w.dir, fileName(day, seq+1))
		if _, err := os.Stat(path); err != nil {
			break
		}
		seq++
	}
	return w.rotateTo(t, seq)
}

// rotateTo 切换到指定日期与序号的文件。调用方须持有锁。
func (w *RotatingFile) rotateTo(t time.Time, seq int) error {
	if w.file != nil {
		_ = w.file.Close()
	}
	day := t.Format(time.DateOnly)
	path := filepath.Join(w.dir, fileName(day, seq))
	// 0o640：日志可能含请求路径与错误详情，不对同机其他用户开放。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("打开日志文件 %s: %w", path, err)
	}
	size := int64(0)
	if info, statErr := f.Stat(); statErr == nil {
		size = info.Size()
	}
	w.file, w.day, w.seq, w.written = f, day, seq, size
	return nil
}

// cleanup 删除超出保留期的日志文件。
//
// 失败只是静默返回：清理不成功不该影响写入，最坏结果是磁盘上多留几天的文件。
func (w *RotatingFile) cleanup() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -w.retainDays).Format(time.DateOnly)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		day, _, ok := ParseFileName(e.Name())
		if !ok || day >= cutoff {
			continue
		}
		_ = os.Remove(filepath.Join(w.dir, e.Name()))
	}
}

// fileName 拼出日志文件名。seq 为 0 时不带序号。
func fileName(day string, seq int) string {
	if seq <= 0 {
		return FilePrefix + day + FileSuffix
	}
	return fmt.Sprintf("%s%s.%d%s", FilePrefix, day, seq, FileSuffix)
}

// ParseFileName 从文件名解析日期与序号，不是日志文件时 ok 为 false。
//
// 供读取端复用：哪些文件算日志、怎么排序，只有一个答案。
func ParseFileName(name string) (day string, seq int, ok bool) {
	m := fileNamePattern.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	if m[2] != "" {
		for _, c := range m[2] {
			seq = seq*10 + int(c-'0')
		}
	}
	return m[1], seq, true
}

// ListFiles 返回目录下的日志文件名，按日期与序号倒序（最新的在前）。
func ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取日志目录: %w", err)
	}
	type item struct {
		name string
		day  string
		seq  int
	}
	items := make([]item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		day, seq, ok := ParseFileName(e.Name())
		if !ok {
			continue
		}
		items = append(items, item{e.Name(), day, seq})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].day != items[j].day {
			return items[i].day > items[j].day
		}
		return items[i].seq > items[j].seq
	})
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.name)
	}
	return out, nil
}

// SafeFileName 校验调用方给的文件名确实是本目录下的日志文件。
//
// 下载与读取接口都要用它：文件名来自请求，必须挡住 ../ 与任意路径。
func SafeFileName(name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", false
	}
	if _, _, ok := ParseFileName(name); !ok {
		return "", false
	}
	return name, true
}
