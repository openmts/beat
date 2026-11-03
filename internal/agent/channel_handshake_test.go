package agent

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestChannelHandshakeHonorsContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case connection := <-accepted:
			_ = connection.Close()
		default:
		}
	})
	channel, err := NewChannel("http://"+listener.Addr().String(), "token", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- channel.runOnce(ctx) }()
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("canceled WebSocket handshake returned success")
		}
	case <-time.After(time.Second):
		_ = listener.Close()
		select {
		case connection := <-accepted:
			_ = connection.Close()
		case <-time.After(time.Second):
		}
		t.Fatal("WebSocket handshake ignored context cancellation")
	}
}
