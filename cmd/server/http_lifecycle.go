package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
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
	tlsConfig      *tls.Config
	beforeShutdown func()
}

func serveHTTP(ctx context.Context, config httpServerConfig) error {
	listener, err := net.Listen("tcp", config.address)
	if err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	hijacked := newHijackedTracker()
	server := newHTTPServerWithTracker(config.handler, hijacked)
	if config.tlsConfig != nil {
		server.TLSConfig = config.tlsConfig.Clone()
	}
	scheme := "http"
	if server.TLSConfig != nil {
		scheme = "https"
	}
	slog.InfoContext(ctx, "server listening", "address", config.address, "scheme", scheme, "version", buildinfo.Version)
	return (httpLifecycle{server: server, listener: listener, beforeShutdown: config.beforeShutdown, hijacked: hijacked}).run(ctx)
}

// newHTTPServer 固定入口大小策略：请求头上限 32 KiB，请求目标上限 8 KiB。
func newHTTPServer(handler http.Handler) *http.Server {
	return newHTTPServerWithTracker(handler, nil)
}

func newHTTPServerWithTracker(handler http.Handler, tracker *hijackedTracker) *http.Server {
	if tracker != nil {
		handler = hijackedHandler{next: handler, tracker: tracker}
	}
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
	hijacked       *hijackedTracker
}

func (lifecycle httpLifecycle) run(ctx context.Context) error {
	finished := make(chan error, 1)
	serve := func() error { return lifecycle.server.Serve(lifecycle.listener) }
	if lifecycle.server.TLSConfig != nil {
		serve = func() error { return lifecycle.server.ServeTLS(lifecycle.listener, "", "") }
	}
	go func() { finished <- serve() }()
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
	// Shutdown 已停止监听并排空普通请求，此时被劫持连接集合不会再增长。
	var hijackedErr error
	if lifecycle.hijacked != nil {
		hijackedErr = lifecycle.hijacked.closeAndWait(shutdownCtx)
	}
	if !received {
		serveErr = <-finished
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr, closeErr, hijackedErr)
}

// hijackedTracker 补齐 net/http.Shutdown 不处理 WebSocket 等被劫持连接的缺口。
// 只有握手成功后才计入；处理器返回时注销，因此正常短连接不会留在表里。
type hijackedTracker struct {
	mu     sync.Mutex
	active int
	conns  map[net.Conn]struct{}
	idle   chan struct{}
}

func newHijackedTracker() *hijackedTracker {
	idle := make(chan struct{})
	close(idle)
	return &hijackedTracker{conns: make(map[net.Conn]struct{}), idle: idle}
}

func (tracker *hijackedTracker) begin(conn net.Conn) {
	tracker.mu.Lock()
	if tracker.active == 0 {
		tracker.idle = make(chan struct{})
	}
	tracker.active++
	tracker.conns[conn] = struct{}{}
	tracker.mu.Unlock()
}

func (tracker *hijackedTracker) end(conn net.Conn) {
	tracker.mu.Lock()
	delete(tracker.conns, conn)
	tracker.active--
	if tracker.active == 0 {
		close(tracker.idle)
	}
	tracker.mu.Unlock()
}

// closeAndWait 先关闭当前连接并等待处理器返回；超时返回错误，不能静默伪装成优雅停机。
func (tracker *hijackedTracker) closeAndWait(ctx context.Context) error {
	tracker.mu.Lock()
	conns := make([]net.Conn, 0, len(tracker.conns))
	for conn := range tracker.conns {
		conns = append(conns, conn)
	}
	idle := tracker.idle
	tracker.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for hijacked connections: %w", ctx.Err())
	}
}

type hijackedHandler struct {
	next    http.Handler
	tracker *hijackedTracker
}

func (handler hijackedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tracked := &trackedResponseWriter{ResponseWriter: w, tracker: handler.tracker}
	defer tracked.finish()
	handler.next.ServeHTTP(tracked, r)
}

type trackedResponseWriter struct {
	http.ResponseWriter
	tracker  *hijackedTracker
	conn     net.Conn
	hijacked bool
}

func (writer *trackedResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *trackedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	conn, buffer, err := hijacker.Hijack()
	if err == nil {
		writer.conn = conn
		writer.hijacked = true
		writer.tracker.begin(conn)
	}
	return conn, buffer, err
}

func (writer *trackedResponseWriter) finish() {
	if writer.hijacked {
		writer.tracker.end(writer.conn)
	}
}
