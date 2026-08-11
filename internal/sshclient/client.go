package sshclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/beat/backend/internal/model"
)

const (
	sshUser        = "root"
	sshDialTimeout = 10 * time.Second
	sshOutputLimit = 1024 * 1024
)

var errSSHOutputLimit = errors.New("ssh output exceeds limit")

type Connector struct {
	hostKeyCallback ssh.HostKeyCallback
}

type Request struct {
	Node       model.Node
	PrivateKey string
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func New(knownHostsPath string) (*Connector, error) {
	knownHosts, err := NewKnownHosts(knownHostsPath)
	if err != nil {
		return nil, err
	}
	return &Connector{hostKeyCallback: knownHosts.Callback}, nil
}

func (c *Connector) Execute(
	ctx context.Context,
	request Request,
	command string,
) (string, error) {
	signer, err := parseSigner(request.PrivateKey)
	if err != nil {
		return "", err
	}
	client, err := c.dial(ctx, request.Node, signer)
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("create ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	output := &limitedBuffer{limit: sshOutputLimit}
	session.Stdout = output
	session.Stderr = output
	if err := runCommand(ctx, session, command); err != nil {
		return output.String(), err
	}
	return output.String(), nil
}

func (c *Connector) OpenTerminal(
	ctx context.Context,
	request Request,
	stream io.ReadWriteCloser,
) error {
	signer, err := parseSigner(request.PrivateKey)
	if err != nil {
		return err
	}
	client, err := c.dial(ctx, request.Node, signer)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()
	defer func() { _ = stream.Close() }()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open ssh stdin: %w", err)
	}
	writer := &lockedWriter{writer: stream}
	session.Stdout = writer
	session.Stderr = writer
	if err := startShell(session); err != nil {
		return err
	}
	return bridgeTerminal(ctx, session, stdin, stream)
}

func (c *Connector) dial(ctx context.Context, node model.Node, signer ssh.Signer) (*ssh.Client, error) {
	address, err := nodeAddress(node)
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: c.hostKeyCallback,
	}
	raw, err := (&net.Dialer{Timeout: sshDialTimeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial ssh server: %w", err)
	}

	if err := setHandshakeDeadline(ctx, raw); err != nil {
		_ = raw.Close()
		return nil, err
	}
	connection, channels, requests, err := ssh.NewClientConn(raw, address, config)
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("clear ssh deadline: %w", err)
	}
	return ssh.NewClient(connection, channels, requests), nil
}

func nodeAddress(node model.Node) (string, error) {
	if node.Host == "" || node.Port < 1 || node.Port > 65535 {
		return "", errors.New("invalid ssh target")
	}
	return net.JoinHostPort(node.Host, strconv.Itoa(node.Port)), nil
}

func setHandshakeDeadline(ctx context.Context, connection net.Conn) error {
	deadline := time.Now().Add(sshDialTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set ssh deadline: %w", err)
	}
	return nil
}

func startShell(session *ssh.Session) error {
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		return fmt.Errorf("request ssh pty: %w", err)
	}
	if err := session.Shell(); err != nil {
		return fmt.Errorf("start ssh shell: %w", err)
	}
	return nil
}

func bridgeTerminal(
	ctx context.Context,
	session *ssh.Session,
	stdin io.WriteCloser,
	stream io.ReadWriteCloser,
) error {
	inputDone := make(chan error, 1)
	waitDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdin, stream)
		_ = stdin.Close()
		inputDone <- err
	}()
	go func() { waitDone <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Close()
		_ = stream.Close()
		<-waitDone
		<-inputDone
		return ctx.Err()
	case err := <-inputDone:
		_ = session.Close()
		_ = stream.Close()
		waitErr := <-waitDone
		return terminalError(errors.Join(err, waitErr))
	case err := <-waitDone:
		_ = session.Close()
		_ = stream.Close()
		<-inputDone
		return terminalError(err)
	}
}

func runCommand(ctx context.Context, session *ssh.Session, command string) error {
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("run ssh command: %w", err)
		}
		return nil
	}
}

func terminalError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	var exitMissing *ssh.ExitMissingError
	if errors.As(err, &exitMissing) {
		return nil
	}
	return fmt.Errorf("ssh terminal ended: %w", err)
}

func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		return 0, errSSHOutputLimit
	}
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		return remaining, errSSHOutputLimit
	}
	return b.buffer.Write(data)
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}
