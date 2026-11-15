package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	beatv1 "beat/internal/protocol/beatv1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

type Client struct {
	token  string
	conn   *grpc.ClientConn
	stream beatv1.AgentControl_ConnectClient
	sendMu sync.Mutex
}

func NewClient(serverURL, token string, insecureSkipVerify bool) (*Client, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("Agent 接入令牌不能为空")
	}
	target, secure, err := grpcTarget(serverURL)
	if err != nil {
		return nil, err
	}
	transport := credentials.TransportCredentials(insecure.NewCredentials())
	if secure {
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecureSkipVerify}) //nolint:gosec -- explicitly enabled by operator flag
	}
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(transport),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20), grpc.MaxCallSendMsgSize(8<<20)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true}),
	)
	if err != nil {
		return nil, fmt.Errorf("创建 gRPC 连接失败: %w", err)
	}
	return &Client{token: token, conn: conn}, nil
}

func (c *Client) Open(ctx context.Context, hello *beatv1.Hello) (*beatv1.Welcome, error) {
	streamCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
	stream, err := beatv1.NewAgentControlClient(c.conn).Connect(streamCtx)
	if err != nil {
		return nil, fmt.Errorf("建立 Agent 双向流失败: %w", err)
	}
	c.stream = stream
	if err := c.send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_Hello{Hello: hello}}); err != nil {
		return nil, fmt.Errorf("发送 Agent 握手失败: %w", err)
	}
	message, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("等待 Server 握手响应失败: %w", err)
	}
	welcome := message.GetWelcome()
	if welcome == nil {
		return nil, fmt.Errorf("Server 首条响应不是 Welcome")
	}
	return welcome, nil
}

func (c *Client) SendReport(ctx context.Context, report *beatv1.Report) error {
	return c.send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_Report{Report: report}})
}

func (c *Client) SendTaskResult(ctx context.Context, result *beatv1.TaskResult) error {
	return c.send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_TaskResult{TaskResult: result}})
}

func (c *Client) SendHeartbeat(ctx context.Context, message *beatv1.Heartbeat) error {
	return c.send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_Heartbeat{Heartbeat: message}})
}

func (c *Client) SendUpgradeEvent(ctx context.Context, event *beatv1.UpgradeEvent) error {
	return c.send(&beatv1.AgentMessage{Payload: &beatv1.AgentMessage_UpgradeEvent{UpgradeEvent: event}})
}

func (c *Client) Receive() (*beatv1.ServerMessage, error) {
	return c.stream.Recv()
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) send(message *beatv1.AgentMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.stream.Send(message)
}

func grpcTarget(raw string) (target string, secure bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, fmt.Errorf("Server 地址不能为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("解析 Server 地址失败: %w", err)
	}
	switch parsed.Scheme {
	case "http", "grpc":
		secure = false
	case "https", "grpcs":
		secure = true
	default:
		return "", false, fmt.Errorf("不支持的 Server 协议: %s", parsed.Scheme)
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return "", false, fmt.Errorf("Server 地址缺少主机名")
	}
	port := parsed.Port()
	if port == "" {
		port = "9099"
	}
	return net.JoinHostPort(hostname, port), secure, nil
}
