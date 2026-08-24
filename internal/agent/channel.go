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

	// 服务端每 30s 发送 ping；读超时取其 3 倍以容忍网络抖动。
	channelReadTimeout  = 90 * time.Second
	channelWriteTimeout = 30 * time.Second
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

// outgoing 是一条待发送消息；pong 需在任务执行期间也能及时发出，因此读写
// 分离为两个 goroutine。
type outgoing struct{ payload any }

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

	out := make(chan outgoing, 16)
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		c.writeLoop(ctx, conn, out)
	}()

	err = c.readLoop(ctx, conn, out)
	close(out) // 通知写循环退出；conn 由 defer 关闭解除其阻塞
	<-writeDone
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

func (c *Channel) readLoop(
	ctx context.Context,
	conn *websocket.Conn,
	out chan<- outgoing,
) error {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(channelReadTimeout)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}
		var message channelMessage
		if err := websocket.JSON.Receive(conn, &message); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive agent channel message: %w", err)
		}
		switch {
		case message.Type == "ping":
			// 写队列满时丢弃 pong；服务端下轮 ping 会再次触发。
			select {
			case out <- outgoing{payload: pingPongMessage{Type: "pong"}}:
			default:
			}
		case message.Type == "task" && message.Task != nil:
			result := c.executor.ExecuteTask(ctx, *message.Task)
			out <- outgoing{payload: taskResultMessage{Type: "task_result", Result: result}}
		}
	}
}

func (c *Channel) writeLoop(ctx context.Context, conn *websocket.Conn, out <-chan outgoing) {
	for {
		select {
		case message, ok := <-out:
			if !ok {
				return
			}
			if err := sendOutgoing(conn, message.payload); err != nil {
				_ = conn.Close()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func sendOutgoing(conn *websocket.Conn, payload any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(channelWriteTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if err := websocket.JSON.Send(conn, payload); err != nil {
		return fmt.Errorf("send agent channel message: %w", err)
	}
	return nil
}

type channelMessage struct {
	Type string           `json:"type"`
	Task *model.AgentTask `json:"task"`
}

type taskResultMessage struct {
	Type   string                `json:"type"`
	Result model.AgentTaskResult `json:"result"`
}

type pingPongMessage struct {
	Type string `json:"type"`
}
