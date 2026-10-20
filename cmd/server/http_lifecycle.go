package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/buildinfo"
)

const (
	httpShutdownTimeout       = 10 * time.Second
	maximumHeaderBytes        = 32 << 10
	maximumRequestTargetBytes = 8 << 10
)

type httpServerConfig struct {
	address        string
	handler        http.Handler
	beforeShutdown func()
}

func serveHTTP(ctx context.Context, config httpServerConfig) error {
	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	server := newHTTPServer(config.handler)
	slog.InfoContext(ctx, "server listening", "address", config.address, "version", buildinfo.Version)
	return (httpLifecycle{server: server, listener: listener, beforeShutdown: config.beforeShutdown}).run(ctx)
}

// newHTTPServer 固定入口大小策略：请求头上限 32 KiB，请求目标上限 8 KiB。
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: requestTargetHandler(handler), MaxHeaderBytes: maximumHeaderBytes,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
}

// requestTargetHandler 在解析路由之前拒绝超长请求目标，避免无界分配与日志噪声。
func requestTargetHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.RequestURI) > maximumRequestTargetBytes {
			handler.JSONError(w, http.StatusRequestURITooLong, "request target is too long")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type httpLifecycle struct {
	server         *http.Server
	listener       net.Listener
	beforeShutdown func()
}

func (lifecycle httpLifecycle) run(ctx context.Context) error {
	finished := make(chan error, 1)
	go func() { finished <- lifecycle.server.Serve(lifecycle.listener) }()
	var serveErr error
	received := false
	select {
	case <-ctx.Done():
	case serveErr = <-finished:
		received = true
	}
	if lifecycle.beforeShutdown != nil {
		lifecycle.beforeShutdown()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	shutdownErr := lifecycle.server.Shutdown(shutdownCtx)
	var closeErr error
	if shutdownErr != nil {
		closeErr = lifecycle.server.Close()
	}
	if !received {
		serveErr = <-finished
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr, closeErr)
}
