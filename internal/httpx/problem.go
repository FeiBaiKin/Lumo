// Package httpx 提供 HTTP 层的通用件：RFC 9457 错误响应与请求上下文工具。
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// ContentTypeProblem 是 RFC 9457 规定的错误响应媒体类型。
const ContentTypeProblem = "application/problem+json"

// Problem 是 RFC 9457 application/problem+json 的响应体。
//
// 字段命名遵循规范：type / title / status / detail / instance 为标准成员，
// Extensions 承载扩展成员（如字段级校验错误），序列化时平铺到顶层。
type Problem struct {
	// Type 是标识问题类型的 URI 引用，缺省 "about:blank"。
	Type string `json:"type"`
	// Title 是问题类型的简短人类可读摘要。
	Title string `json:"title"`
	// Status 是 HTTP 状态码，与响应状态行一致。
	Status int `json:"status"`
	// Detail 是本次具体发生的可读说明。
	Detail string `json:"detail,omitempty"`
	// Instance 标识本次问题实例，本项目填请求路径。
	Instance string `json:"instance,omitempty"`
	// Extensions 是扩展成员，序列化时平铺到 JSON 顶层。
	Extensions map[string]any `json:"-"`
}

// Error 让 Problem 可直接作为 error 使用与包装。
func (p *Problem) Error() string {
	if p.Detail != "" {
		return fmt.Sprintf("%d %s: %s", p.Status, p.Title, p.Detail)
	}
	return fmt.Sprintf("%d %s", p.Status, p.Title)
}

// MarshalJSON 把标准成员与扩展成员合并为单层对象。
func (p *Problem) MarshalJSON() ([]byte, error) {
	problemType := p.Type
	if problemType == "" {
		problemType = "about:blank"
	}
	title := p.Title
	if title == "" {
		title = http.StatusText(p.Status)
	}

	merged := map[string]any{
		"type":   problemType,
		"title":  title,
		"status": p.Status,
	}
	if p.Detail != "" {
		merged["detail"] = p.Detail
	}
	if p.Instance != "" {
		merged["instance"] = p.Instance
	}
	// 扩展成员不得覆盖标准成员，否则响应会自相矛盾。
	for k, v := range p.Extensions {
		if _, reserved := merged[k]; reserved {
			continue
		}
		merged[k] = v
	}
	return json.Marshal(merged)
}

// NewProblem 构造一个带状态码与说明的 Problem。
func NewProblem(status int, detail string) *Problem {
	return &Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	}
}

// WriteProblem 以 application/problem+json 写出错误响应。
//
// logger 可为 nil；序列化失败时降级为纯文本，保证客户端总能收到可解析的响应。
func WriteProblem(w http.ResponseWriter, r *http.Request, problem *Problem, logger *slog.Logger) {
	if problem == nil {
		problem = NewProblem(http.StatusInternalServerError, "")
	}
	if problem.Status == 0 {
		problem.Status = http.StatusInternalServerError
	}
	if problem.Instance == "" && r != nil {
		problem.Instance = r.URL.Path
	}

	body, err := json.Marshal(problem)
	if err != nil {
		if logger != nil {
			logger.Error("序列化 problem 响应失败", slog.Any("error", err))
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		return
	}

	w.Header().Set("Content-Type", ContentTypeProblem)
	// 错误响应不应被缓存。
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(problem.Status)
	if r != nil && r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// Error 是 WriteProblem 的便捷形式。
func Error(w http.ResponseWriter, r *http.Request, status int, detail string) {
	WriteProblem(w, r, NewProblem(status, detail), nil)
}

// WriteError 把任意 error 转成 problem 响应。
//
// 若 err 链上存在 *Problem 则沿用其状态与说明；否则统一按 500 处理，
// 并且**不把内部错误细节回传客户端**，只记入日志，避免泄漏实现信息。
func WriteError(w http.ResponseWriter, r *http.Request, err error, logger *slog.Logger) {
	var problem *Problem
	if errors.As(err, &problem) {
		WriteProblem(w, r, problem, logger)
		return
	}
	if logger != nil {
		logger.Error("未处理的请求错误", slog.Any("error", err))
	}
	WriteProblem(w, r, NewProblem(http.StatusInternalServerError, ""), logger)
}

// WriteJSON 以 application/json 写出成功响应。
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		Error(w, r, http.StatusInternalServerError, "")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if r != nil && r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}
