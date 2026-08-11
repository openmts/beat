package sshclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/beat/backend/internal/model"
)

func TestConnectorAndAddressErrors(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected empty known hosts path error")
	}
	privateKey, _ := generatePrivateKey(t)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	for _, node := range []model.Node{{Port: 22}, {Host: "127.0.0.1", Port: 0}, {Host: "127.0.0.1", Port: 65536}} {
		_, err := connector.Execute(context.Background(), Request{Node: node, PrivateKey: privateKey}, "true")
		if err == nil || !strings.Contains(err.Error(), "invalid ssh target") {
			t.Fatalf("error = %v, want invalid target", err)
		}
	}
	_, err = connector.Execute(context.Background(), Request{
		Node: model.Node{Host: "127.0.0.1", Port: 1}, PrivateKey: privateKey,
	}, "true")
	if err == nil || !strings.Contains(err.Error(), "dial ssh server") {
		t.Fatalf("error = %v, want dial error", err)
	}
	streamA, streamB := net.Pipe()
	_ = streamB.Close()
	if err := connector.OpenTerminal(context.Background(), Request{PrivateKey: "invalid"}, streamA); err == nil {
		t.Fatal("expected private key error")
	}
}

func TestLimitedBuffer(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	if n, err := buffer.Write([]byte("ab")); err != nil || n != 2 {
		t.Fatalf("write = %d, %v", n, err)
	}
	if n, err := buffer.Write([]byte("cdef")); !errors.Is(err, errSSHOutputLimit) || n != 2 {
		t.Fatalf("limited write = %d, %v", n, err)
	}
	if n, err := buffer.Write([]byte("x")); !errors.Is(err, errSSHOutputLimit) || n != 0 {
		t.Fatalf("full write = %d, %v", n, err)
	}
	if buffer.String() != "abcd" {
		t.Fatalf("buffer = %q", buffer.String())
	}
}

func TestLockedWriter(t *testing.T) {
	var output bytes.Buffer
	writer := &lockedWriter{writer: &output}
	if n, err := writer.Write([]byte("data")); err != nil || n != 4 || output.String() != "data" {
		t.Fatalf("write = %d, %v, %q", n, err, output.String())
	}
}

func TestTerminalError(t *testing.T) {
	for _, err := range []error{nil, io.EOF, net.ErrClosed, &ssh.ExitMissingError{}} {
		if got := terminalError(err); got != nil {
			t.Fatalf("terminalError(%v) = %v", err, got)
		}
	}
	if err := terminalError(errors.New("failed")); err == nil || !strings.Contains(err.Error(), "ssh terminal ended") {
		t.Fatalf("error = %v", err)
	}
}

func TestSetHandshakeDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := setHandshakeDeadline(ctx, a); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
}

func TestConnectorSSHHandshakeFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = connection.Close() }()
	}()
	privateKey, _ := generatePrivateKey(t)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	_, err = connector.Execute(context.Background(), request, "uptime")
	if err == nil || !strings.Contains(err.Error(), "ssh handshake") {
		t.Fatalf("error = %v, want handshake failure", err)
	}
}

func TestConnectorOpenTerminalRejectsPTY(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startPTYRejectingServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	serverSide, _ := net.Pipe()
	defer func() { _ = serverSide.Close() }()
	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	if err := connector.OpenTerminal(context.Background(), request, serverSide); err == nil ||
		!strings.Contains(err.Error(), "ssh pty") {
		t.Fatalf("error = %v, want PTY rejection", err)
	}
}

func TestConnectorOpenTerminalRejectsShell(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startShellRejectingServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	serverSide, _ := net.Pipe()
	defer func() { _ = serverSide.Close() }()
	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	if err := connector.OpenTerminal(context.Background(), request, serverSide); err == nil ||
		!strings.Contains(err.Error(), "ssh shell") {
		t.Fatalf("error = %v, want shell rejection", err)
	}
}

func startPTYRejectingServer(t *testing.T, hostSigner ssh.Signer) net.Listener {
	t.Helper()
	return startRequestRejectingServer(t, hostSigner, map[string]bool{"pty-req": false})
}

func startShellRejectingServer(t *testing.T, hostSigner ssh.Signer) net.Listener {
	t.Helper()
	return startRequestRejectingServer(t, hostSigner, map[string]bool{"pty-req": true, "shell": false})
}

func startRequestRejectingServer(t *testing.T, hostSigner ssh.Signer, replies map[string]bool) net.Listener {
	t.Helper()
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, _ ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					_ = conn.Close()
					return
				}
				go ssh.DiscardRequests(requests)
				for channelRequest := range channels {
					if channelRequest.ChannelType() != "session" {
						_ = channelRequest.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					channel, channelRequests, err := channelRequest.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer func() { _ = channel.Close() }()
						for request := range channelRequests {
							_ = request.Reply(replies[request.Type], nil)
						}
					}()
				}
				_ = serverConn.Close()
			}()
		}
	}()
	return listener
}
