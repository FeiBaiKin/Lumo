// Package logs 读取写在磁盘上的运行日志，供后台「日志」页查询与下载。
//
// 只读不写：日志的产生在 internal/logging，本包只解析它写下的 JSON 行。
// 两边共用 logging 里的文件名约定（FilePrefix / ParseFileName / SafeFileName），
// 免得「哪些文件算日志」出现第二个答案。
package logs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FeiBaiKin/lumo/internal/logging"
)

// 扫描上限。日志文件可能有上百 MB，一次查询不能把整个磁盘读进内存。
//
// 触顶时结果标记为截断，让站长知道该缩小时间范围，而不是以为「就这些」。
const (
	maxScanLines = 500_000
	maxWindow    = 2000
	maxLineBytes = 1 << 20
)

// ErrBadFileName 表示请求的文件名不是本目录下的日志文件。
var ErrBadFileName = errors.New("不是合法的日志文件名")

// Entry 是一条日志。
type Entry struct {
	Time  time.Time      `json:"time"`
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	Attrs map[string]any `json:"attrs,omitempty"`
	// File 是这条日志所在的文件名，便于站长按文件下载原文。
	File string `json:"file"`
}

// Query 是一次查询的条件。
type Query struct {
	// Level 是最低级别（debug/info/warn/error），空表示不限。
	//
	// 用「最低」而不是「精确匹配」：排障时问的是「有没有出错」，
	// 选 warn 却看不到 error 会让人以为一切正常。
	Level string
	// Q 是关键词，在消息与全部属性值里做大小写不敏感的子串匹配。
	Q string
	// From、To 限定时间范围，零值表示不限。
	From time.Time
	To   time.Time
	// File 限定只查某个日志文件，空表示全部。
	File string
	// Page 从 1 起，Size 是每页条数。
	Page int
	Size int
}

// Result 是一次查询的结果。
type Result struct {
	Items []Entry `json:"items"`
	Total int     `json:"total"`
	Page  int     `json:"page"`
	Size  int     `json:"size"`
	// Truncated 为真表示触到扫描上限，更早的日志没有纳入统计。
	Truncated bool `json:"truncated"`
}

// FileInfo 描述一个日志文件。
type FileInfo struct {
	Name string `json:"name"`
	// Day 是文件所属日期，形如 2026-09-21。
	Day       string    `json:"day"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"modTime"`
	SizeHuman string    `json:"sizeHuman"`
}

// Reader 从日志目录读取日志。
type Reader struct {
	dir string
}

// NewReader 构造读取器。
func NewReader(dir string) *Reader { return &Reader{dir: dir} }

// Dir 返回日志目录。
func (r *Reader) Dir() string { return r.dir }

// Files 列出日志文件，最新的在前。
func (r *Reader) Files() ([]FileInfo, error) {
	names, err := logging.ListFiles(r.dir)
	if err != nil {
		return nil, err
	}
	out := make([]FileInfo, 0, len(names))
	for _, name := range names {
		day, _, _ := logging.ParseFileName(name)
		info := FileInfo{Name: name, Day: day}
		if st, statErr := os.Stat(filepath.Join(r.dir, name)); statErr == nil {
			info.Size = st.Size()
			info.ModTime = st.ModTime()
			info.SizeHuman = humanSize(st.Size())
		}
		out = append(out, info)
	}
	return out, nil
}

// Open 打开一个日志文件供下载；文件名非法或不存在时返回错误。
func (r *Reader) Open(name string) (*os.File, os.FileInfo, error) {
	safe, ok := logging.SafeFileName(name)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q", ErrBadFileName, name)
	}
	f, err := os.Open(filepath.Join(r.dir, safe)) //nolint:gosec // 文件名经 SafeFileName 校验，且限定在日志目录内
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, st, nil
}

// Query 按条件查询日志，最新的在前。
func (r *Reader) Query(q Query) (*Result, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 {
		q.Size = 50
	}
	if q.Size > 200 {
		q.Size = 200
	}
	need := min(q.Page*q.Size, maxWindow)

	names, err := logging.ListFiles(r.dir)
	if err != nil {
		return nil, err
	}
	names = filterFiles(names, q)

	minLevel := levelValue(q.Level)
	keyword := strings.ToLower(strings.TrimSpace(q.Q))

	collected := make([]Entry, 0, need)
	scanned := 0
	truncated := false

	for _, name := range names {
		if len(collected) >= need {
			break
		}
		batch, lines, cut, scanErr := r.scanFile(name, q, minLevel, keyword, need-len(collected))
		if scanErr != nil {
			// 单个文件读坏不该让整页查不出来：跳过它，继续更早的文件。
			continue
		}
		scanned += lines
		if cut {
			truncated = true
		}
		// 文件内是正序，倒着追加就成了「最新在前」。
		for i := len(batch) - 1; i >= 0; i-- {
			collected = append(collected, batch[i])
		}
		if scanned >= maxScanLines {
			truncated = true
			break
		}
	}

	total := len(collected)
	start := min((q.Page-1)*q.Size, total)
	end := min(start+q.Size, total)
	return &Result{
		Items:     collected[start:end],
		Total:     total,
		Page:      q.Page,
		Size:      q.Size,
		Truncated: truncated,
	}, nil
}

// scanFile 扫描一个文件，返回其中最新的 want 条匹配项（正序）。
func (r *Reader) scanFile(name string, q Query, minLevel int, keyword string, want int) (
	entries []Entry, lines int, truncated bool, err error,
) {
	f, err := os.Open(filepath.Join(r.dir, name)) //nolint:gosec // name 来自 ListFiles，形态已受约束
	if err != nil {
		return nil, 0, false, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	buf := make([]Entry, 0, want)
	for sc.Scan() {
		lines++
		if lines > maxScanLines {
			truncated = true
			break
		}
		e, ok := parseLine(sc.Bytes())
		if !ok || !match(&e, q, minLevel, keyword) {
			continue
		}
		e.File = name
		buf = append(buf, e)
		// 只留最近 want 条：文件是正序追加的，越往后越新。
		// 攒到两倍才裁一次，避免每行都重新分配。
		if len(buf) > want*2 {
			buf = append(buf[:0:0], buf[len(buf)-want:]...)
		}
	}
	if scanErr := sc.Err(); scanErr != nil && !isTooLong(scanErr) {
		return nil, lines, truncated, scanErr
	}
	if len(buf) > want {
		buf = buf[len(buf)-want:]
	}
	return buf, lines, truncated, nil
}

// isTooLong 报告扫描错误是否是「某一行超长」。
//
// 单行超长不该让整个文件读不出来：把前面已经读到的结果照常返回。
func isTooLong(err error) bool {
	return err != nil && strings.Contains(err.Error(), "token too long")
}

// parseLine 解析一行 JSON 日志。
//
// 不是合法 JSON 的行直接跳过：控制台那一路可能是 text 格式，
// 站长也可能往目录里放了别的东西。
func parseLine(line []byte) (Entry, bool) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return Entry{}, false
	}
	e := Entry{Attrs: make(map[string]any, len(raw))}
	for k, v := range raw {
		switch k {
		case "time":
			if s, ok := v.(string); ok {
				if t, parseErr := time.Parse(time.RFC3339Nano, s); parseErr == nil {
					e.Time = t
				}
			}
		case "level":
			e.Level, _ = v.(string)
		case "msg":
			e.Msg, _ = v.(string)
		default:
			e.Attrs[k] = v
		}
	}
	if e.Level == "" && e.Msg == "" {
		return Entry{}, false
	}
	if len(e.Attrs) == 0 {
		e.Attrs = nil
	}
	return e, true
}

// match 判断一条日志是否符合查询条件。
func match(e *Entry, q Query, minLevel int, keyword string) bool {
	if minLevel > 0 && levelValue(e.Level) < minLevel {
		return false
	}
	if !q.From.IsZero() && e.Time.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && e.Time.After(q.To) {
		return false
	}
	if keyword == "" {
		return true
	}
	if strings.Contains(strings.ToLower(e.Msg), keyword) {
		return true
	}
	for k, v := range e.Attrs {
		if strings.Contains(strings.ToLower(k), keyword) {
			return true
		}
		if strings.Contains(strings.ToLower(fmt.Sprint(v)), keyword) {
			return true
		}
	}
	return false
}

// filterFiles 按查询条件缩小要扫的文件范围。
//
// 文件名带日期，时间范围可以在打开文件之前就把大部分排除掉。
func filterFiles(names []string, q Query) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if q.File != "" && name != q.File {
			continue
		}
		day, _, ok := logging.ParseFileName(name)
		if !ok {
			continue
		}
		if !q.From.IsZero() && day < q.From.Format(time.DateOnly) {
			continue
		}
		if !q.To.IsZero() && day > q.To.Format(time.DateOnly) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// levelValue 把级别名转成可比较的序数；无法识别时为 0（不参与过滤）。
//
// 加 100 是为了让 DEBUG（slog 里是 -4）也落在正数区间，
// 这样「0 表示不过滤」才不会和真实级别撞上。
func levelValue(level string) int {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return int(slog.LevelDebug) + 100
	case "INFO":
		return int(slog.LevelInfo) + 100
	case "WARN", "WARNING":
		return int(slog.LevelWarn) + 100
	case "ERROR":
		return int(slog.LevelError) + 100
	default:
		return 0
	}
}

// humanSize 把字节数说成人话。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
