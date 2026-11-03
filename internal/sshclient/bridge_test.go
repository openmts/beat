package sshclient

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type failingReadCloser struct {
	err    error
	closed bool
}

func (stream *failingReadCloser) Read([]byte) (int, error) {
	return 0, stream.err
}

func (stream *failingReadCloser) Write(data []byte) (int, error) {
	return len(data), nil
}

func (stream *failingReadCloser) Close() error {
	stream.closed = true
	return nil
}

func TestBridgeTerminalStopsWhenRemoteCloses(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startClosingShellServer(t, hostSigner)
	session, stdin := openBridgeSession(t, listener, privateKey)
	stream, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	if err := bridgeTerminal(t.Context(), session, stdin, stream); err != nil {
		t.Fatalf("bridge error = %v, want nil", err)
	}
}

func TestBridgeTerminalStopsWhenContextIsCanceled(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startRequestRejectingServer(t, hostSigner, map[string]bool{"pty-req": true, "shell": true})
	session, stdin := openBridgeSession(t, listener, privateKey)
	stream, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := bridgeTerminal(ctx, session, stdin, stream); !errors.Is(err, context.Canceled) {
		t.Fatalf("bridge error = %v, want canceled", err)
	}
}

func TestBridgeTerminalHandlesInputStreamFailure(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startRequestRejectingServer(t, hostSigner, map[string]bool{"pty-req": true, "shell": true})
	session, stdin := openBridgeSession(t, listener, privateKey)
	streamErr := errors.New("input failed")
	stream := &failingReadCloser{err: streamErr}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := bridgeTerminal(ctx, session, stdin, stream)
	if err != nil && !errors.Is(err, streamErr) {
		t.Fatalf("bridge error = %v, want input failure or clean close", err)
	}
	if !stream.closed {
		t.Fatal("input stream not closed")
	}
}

func openBridgeSession(t *testing.T, listener net.Listener, privateKey string) (*ssh.Session, io.WriteCloser) {
	t.Helper()
	signer, err := parseSigner(privateKey)
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	client, err := connector.dial(t.Context(), testNode(t, listener.Addr().String()), signer)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		t.Fatalf("create session: %v", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		t.Fatalf("open stdin: %v", err)
	}
	if err = startShell(session); err != nil {
		_ = session.Close()
		_ = client.Close()
		t.Fatalf("start shell: %v", err)
	}
	t.Cleanup(func() { _ = session.Close(); _ = client.Close() })
	return session, stdin
}

func startClosingShellServer(t *testing.T, hostSigner ssh.Signer) net.Listener {
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
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				serverConn, channels, requests, handshakeErr := ssh.NewServerConn(connection, config)
				if handshakeErr != nil {
					_ = connection.Close()
					return
				}
				go ssh.DiscardRequests(requests)
				for channelRequest := range channels {
					channel, channelRequests, acceptErr := channelRequest.Accept()
					if acceptErr != nil {
						continue
					}
					go func() {
						defer func() { _ = channel.Close() }()
						for request := range channelRequests {
							accepted := request.Type == "pty-req" || request.Type == "shell"
							_ = request.Reply(accepted, nil)
							if request.Type == "shell" {
								return
							}
						}
					}()
				}
				_ = serverConn.Close()
			}()
		}
	}()
	return listener
}
