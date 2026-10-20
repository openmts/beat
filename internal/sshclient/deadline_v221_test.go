package sshclient

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// failingDeadlineConn 只实现握手期用到的 net.Conn 子集，用固定的 SetDeadline 失败
// 证明握手超时无法设置时会被上报，而不是带着无界握手继续连接。
type failingDeadlineConn struct {
	net.Conn
	err error
}

func (conn failingDeadlineConn) SetDeadline(time.Time) error { return conn.err }

func TestSetHandshakeDeadlineReportsUnsupportedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	expected := errors.New("deadline unsupported")
	err := setHandshakeDeadline(ctx, failingDeadlineConn{err: expected})
	if !errors.Is(err, expected) {
		t.Fatalf("expected the connection failure, got %v", err)
	}
}
