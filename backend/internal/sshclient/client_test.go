package sshclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/beat/backend/internal/model"
)

func generatePrivateKey(t *testing.T) (string, ssh.Signer) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	return string(pemData), signer
}

func startSSHServer(t *testing.T, hostSigner ssh.Signer) net.Listener {
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
			go serveSSHConnection(conn, config)
		}
	}()
	return listener
}

func serveSSHConnection(raw net.Conn, config *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		_ = raw.Close()
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
		go serveSSHSession(channel, channelRequests)
	}
	_ = serverConn.Close()
}

func serveSSHSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	for request := range requests {
		switch request.Type {
		case "pty-req":
			_ = request.Reply(true, nil)
		case "shell":
			_ = request.Reply(true, nil)
			serveShell(channel)
			return
		case "exec":
			var payload struct{ Command string }
			_ = ssh.Unmarshal(request.Payload, &payload)
			_ = request.Reply(true, nil)
			if payload.Command == "sleep" {
				time.Sleep(100 * time.Millisecond)
			}
			_, _ = io.WriteString(channel, "ran:"+payload.Command)
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			_ = channel.Close()
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func serveShell(channel ssh.Channel) {
	buffer := make([]byte, 128)
	for {
		n, err := channel.Read(buffer)
		if n > 0 {
			_, _ = channel.Write(buffer[:n])
			if bytes.Contains(buffer[:n], []byte("exit\n")) {
				_ = channel.Close()
				return
			}
		}
		if err != nil {
			_ = channel.Close()
			return
		}
	}
}

func testNode(t *testing.T, address string) model.Node {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return model.Node{ID: "node", Name: "node", Host: host, Port: port}
}

func TestConnectorExecute(t *testing.T) {
	privateKey, clientSigner := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startSSHServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}

	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	output, err := connector.Execute(context.Background(), request, "uptime")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output != "ran:uptime" {
		t.Fatalf("output = %q, want %q", output, "ran:uptime")
	}
	_ = clientSigner
}

func TestConnectorOpenTerminal(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startSSHServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	serverSide, clientSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close() })
	errCh := make(chan error, 1)
	go func() {
		request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
		errCh <- connector.OpenTerminal(context.Background(), request, serverSide)
	}()

	if err := clientSide.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := io.WriteString(clientSide, "exit\n"); err != nil {
		t.Fatalf("write terminal: %v", err)
	}
	output, err := io.ReadAll(clientSide)
	if err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("read terminal: %v", err)
	}
	if !strings.Contains(string(output), "exit") {
		t.Fatalf("terminal output = %q", output)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("terminal: %v", err)
	}
}

func TestConnectorRejectsInvalidNode(t *testing.T) {
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	request := Request{Node: model.Node{Host: "", Port: 22}, PrivateKey: "invalid"}
	_, err = connector.Execute(context.Background(), request, "uptime")
	if err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("error = %v, want private key error", err)
	}
}

func TestConnectorExecuteCanceled(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startSSHServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	if _, err := connector.Execute(ctx, request, "sleep"); err == nil ||
		(!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout")) {
		t.Fatalf("error = %v, want deadline or timeout", err)
	}
}

func TestConnectorOpenTerminalCanceled(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startSSHServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	serverSide, clientSide := net.Pipe()
	defer func() { _ = clientSide.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
		done <- connector.OpenTerminal(ctx, request, serverSide)
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}

func TestConnectorExecuteCommandFailure(t *testing.T) {
	privateKey, _ := generatePrivateKey(t)
	_, hostSigner := generatePrivateKey(t)
	listener := startFailingCommandServer(t, hostSigner)
	connector, err := New(filepathForTest(t))
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	request := Request{Node: testNode(t, listener.Addr().String()), PrivateKey: privateKey}
	output, err := connector.Execute(context.Background(), request, "fail")
	if err == nil || !strings.Contains(err.Error(), "run ssh command") {
		t.Fatalf("error = %v, want command failure", err)
	}
	if output != "failure output" {
		t.Fatalf("output = %q, want partial output", output)
	}
}

func startFailingCommandServer(t *testing.T, hostSigner ssh.Signer) net.Listener {
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
						for request := range channelRequests {
							if request.Type != "exec" {
								_ = request.Reply(false, nil)
								continue
							}
							_ = request.Reply(true, nil)
							_, _ = io.WriteString(channel, "failure output")
							_, _ = channel.SendRequest("exit-status", false,
								ssh.Marshal(struct{ Status uint32 }{1}))
							_ = channel.Close()
							return
						}
					}()
				}
				_ = serverConn.Close()
			}()
		}
	}()
	return listener
}

func filepathForTest(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s/ssh/known_hosts", t.TempDir())
}
