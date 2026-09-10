package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/FeiBaiKin/lumo/internal/config"
)

// Server 包装 http.Server 与其生命周期管理。
type Server struct {
	http   *http.Server
	logger *slog.Logger
	cfg    *config.ServerConfig
}

// New 构造 Server。
func New(handler http.Handler, cfg *config.ServerConfig, logger *slog.Logger) *Server {
	return &Server{
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		logger: logger,
		cfg:    cfg,
	}
}

// Run 启动服务并阻塞，直到 ctx 被取消或服务异常退出。
//
// ctx 取消后停止接收新连接，并给在途请求留出 ShutdownTimeout 的收尾时间。
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("HTTP 服务异常退出: %w", err)
		}
		return nil
	case <-ctx.Done():
		if s.logger != nil {
			s.logger.Info("收到退出信号，正在关闭")
		}
	}

	// 关闭用独立 context：ctx 已被取消，不能再用它约束收尾时间。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭失败: %w", err)
	}
	return nil
}
