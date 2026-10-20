package sshclient

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestCancellationInterruptsSilentSSHHandshake(t *testing.T) {
	listener, accepted := silentSSHListener(t)
	privateKey, _ := generatePrivateKey(t)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	go func() {
		_, err := connector.Execute(ctx, request, "true")
		result <- err
	}()
	var connection net.Conn
	select {
	case connection = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("SSH connection was not opened")
	}
	t.Cleanup(func() { _ = connection.Close() })
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handshake cancellation was hidden: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancellation waited for SSH handshake timeout")
	}
	if _, err := io.Copy(io.Discard, connection); err != nil {
		t.Fatal(err)
	}
}

func silentSSHListener(t *testing.T) (net.Listener, <-chan net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			accepted <- connection
		}
	}()
	return listener, accepted
}
