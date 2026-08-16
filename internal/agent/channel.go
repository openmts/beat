package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
)

const (
	channelPath       = "/api/v1/agents/ws"
	channelBackoffMax = 30 * time.Second
	channelBackoffMin = time.Second
)

// TaskExecutor runs a single agent task and reports its outcome.
type TaskExecutor interface {
	ExecuteTask(context.Context, model.AgentTask) model.AgentTaskResult
}

// Channel maintains a resilient WebSocket connection to the server for
// receiving and reporting agent tasks.
type Channel struct {
	location *url.URL
	origin   *url.URL
	token    string
	executor TaskExecutor
	onError  func(error)
}

func NewChannel(serverURL, token string, executor TaskExecutor) (*Channel, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	parsed.Scheme = strings.ReplaceAll(parsed.Scheme, "http", "ws")
	parsed.Path = strings.TrimRight(parsed.Path, "/") + channelPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	location := *parsed
	return &Channel{location: &location, origin: &location, token: token, executor: executor}, nil
}

// SetOnError registers a callback invoked for transient connection errors.
func (c *Channel) SetOnError(onError func(error)) {
	c.onError = onError
}

// Run keeps the channel connected until ctx is cancelled.
func (c *Channel) Run(ctx context.Context) error {
	backoff := channelBackoffMin
	for {
		runDone := make(chan error, 1)
		go func() { runDone <- c.runOnce(ctx) }()
		select {
		case err := <-runDone:
			if err == nil {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			if c.onError != nil {
				c.onError(err)
			}
		case <-ctx.Done():
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > channelBackoffMax {
			backoff = channelBackoffMax
		}
	}
}

func (c *Channel) runOnce(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)
	conn, err := websocket.DialConfig(&websocket.Config{
		Location: c.location,
		Origin:   c.origin,
		Header:   header,
		Version:  websocket.ProtocolVersionHybi13,
		Dialer:   &net.Dialer{Timeout: 10 * time.Second},
	})
	if err != nil {
		return fmt.Errorf("connect agent channel: %w", err)
	}
	defer func() { _ = conn.Close() }()
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	defer close(stop)

	for {
		var message channelMessage
		if err := websocket.JSON.Receive(conn, &message); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive agent channel message: %w", err)
		}
		if message.Type != "task" || message.Task == nil {
			continue
		}
		result := c.executor.ExecuteTask(ctx, *message.Task)
		if err := websocket.JSON.Send(conn, taskResultMessage{Type: "task_result", Result: result}); err != nil {
			return fmt.Errorf("send agent task result: %w", err)
		}
	}
}

type channelMessage struct {
	Type string           `json:"type"`
	Task *model.AgentTask `json:"task"`
}

type taskResultMessage struct {
	Type   string                `json:"type"`
	Result model.AgentTaskResult `json:"result"`
}
