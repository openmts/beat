package service

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/sshclient"
)

// blockingConnector 在 OpenTerminal 中阻塞直至 ctx 取消，模拟活跃 SSH 桥接。
type blockingConnector struct {
	released chan struct{}
}

func (b *blockingConnector) OpenTerminal(
	ctx context.Context,
	_ sshclient.Request,
	stream io.ReadWriteCloser,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.released:
		return nil
	}
}

func (b *blockingConnector) Execute(_ context.Context, _ sshclient.Request, _ string) (string, error) {
	return "", nil
}

type nopStream struct {
	closed chan struct{}
	once   sync.Once
}

func (s *nopStream) Read(_ []byte) (int, error) { return 0, io.EOF }

func (s *nopStream) Write(p []byte) (int, error) { return len(p), nil }

func (s *nopStream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestTerminalSessionLifecycleTracking(t *testing.T) {
	connector := &blockingConnector{released: make(chan struct{})}
	service, node := setupTerminalServiceWithConnector(t, "private", connector)
	stream := &nopStream{closed: make(chan struct{})}

	done := make(chan error, 1)
	go func() { done <- service.OpenTerminal(context.Background(), node.ID, stream) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && service.ActiveSessions() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := service.ActiveSessions(); got != 1 {
		t.Fatalf("active sessions = %d while open, want 1", got)
	}

	// CloseAll 必须取消桥接上下文并关闭流
	service.CloseAll()
	select {
	case <-stream.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("stream was not closed by CloseAll")
	}
	if err := <-done; err == nil {
		t.Fatal("OpenTerminal should return the cancellation error")
	}

	final := service.ActiveSessions()
	if final != 0 {
		t.Fatalf("active sessions after close = %d, want 0", final)
	}
}

func TestTerminalSessionUnregistersOnNormalExit(t *testing.T) {
	connector := &blockingConnector{released: make(chan struct{})}
	close(connector.released)
	service, node := setupTerminalServiceWithConnector(t, "private", connector)

	if err := service.OpenTerminal(context.Background(), node.ID,
		&nopStream{closed: make(chan struct{})}); err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	if got := service.ActiveSessions(); got != 0 {
		t.Fatalf("active sessions after normal exit = %d, want 0", got)
	}
}

// 二次 CloseAll 必须幂等。
func TestTerminalCloseAllIdempotent(t *testing.T) {
	service := &TerminalService{sessions: make(map[*terminalSession]struct{})}
	service.CloseAll()
	service.CloseAll()
	if got := service.ActiveSessions(); got != 0 {
		t.Fatalf("active sessions = %d, want 0", got)
	}
}
