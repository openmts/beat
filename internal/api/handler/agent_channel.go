package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
)

const (
	agentTaskResultMessage = "task_result"
	agentChannelPingEvery  = 30 * time.Second
	agentChannelWriteQueue = 16
	// 读超时为 ping 周期的 3 倍；agent 收到 ping 会回复 pong 续期。
	agentChannelReadTimeout  = 90 * time.Second
	agentChannelWriteTimeout = 30 * time.Second
)

// AgentChannelOperations exposes the pieces the agent channel needs.
type AgentChannelOperations interface {
	EnqueueAuthorizedKeyTask(context.Context, string, string) (*model.AgentTask, error)
	RegisterAgent(context.Context, string) ([]model.AgentTask, error)
	CompleteAgentTask(context.Context, model.AgentTaskResult) error
	// RefreshCustomMetrics enqueues a registration task so the agent rebuilds
	// its local custom metric registry on every (re)connect.
	RefreshCustomMetrics(context.Context, string) (*model.AgentTask, error)
}

type AgentChannelHandler struct {
	operations     AgentChannelOperations
	hub            *service.AgentHub
	verifyIdentity func(context.Context, model.AgentIdentity) error
}

func NewAgentChannelHandler(operations AgentChannelOperations, hub *service.AgentHub) *AgentChannelHandler {
	return &AgentChannelHandler{operations: operations, hub: hub}
}

// agentWebSocketConn adapts a websocket connection for the AgentHub and pumps
// outbound task messages from a channel.
type agentWebSocketConn struct {
	conn      *websocket.Conn
	writeCh   chan []byte
	authorize func(context.Context) error
	binding   string
}

func (c *agentWebSocketConn) WriteTask(ctx context.Context, task model.AgentTask) error {
	if c.authorize != nil {
		if err := c.authorize(ctx); err != nil {
			return err
		}
	}
	message, err := service.NewTaskMessage(task)
	if err != nil {
		return err
	}
	select {
	case c.writeCh <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *agentWebSocketConn) Close() error {
	return c.conn.Close()
}

// HandleAgentWebSocket upgrades the HTTP request to a WebSocket for an agent.
// The AgentAuth middleware must have run first so the agent identity is
// available in the request context.
func (h *AgentChannelHandler) HandleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.operations == nil {
		JSONError(w, http.StatusServiceUnavailable, "agent channel is unavailable")
		return
	}

	server := websocket.Server{
		Handshake: agentChannelHandshake,
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			h.serveConnection(r.Context(), identity.NodeID, conn)
		}),
	}
	server.ServeHTTP(w, r)
}

func (h *AgentChannelHandler) serveConnection(ctx context.Context, nodeID string, conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	wrapped := &agentWebSocketConn{conn: conn, writeCh: make(chan []byte, agentChannelWriteQueue)}
	if identity, ok := middleware.AgentIdentity(ctx); ok && h.verifyIdentity != nil {
		wrapped.binding = agentCredentialBinding(identity)
		wrapped.authorize = func(check context.Context) error { return h.verifyIdentity(check, identity) }
		if err := wrapped.authorize(ctx); err != nil {
			cancel()
			_ = conn.Close()
			return
		}
	}
	h.hub.Register(nodeID, wrapped)
	defer h.hub.Unregister(nodeID, wrapped)
	writeDone, readDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(writeDone); writeLoop(ctx, wrapped); cancel(); _ = conn.Close() }()
	go func() { defer close(readDone); h.receiveResults(ctx, nodeID, conn); cancel(); _ = conn.Close() }()
	defer func() { cancel(); _ = conn.Close(); <-writeDone; <-readDone }()
	if err := h.replayAgentTasks(ctx, nodeID, wrapped); err != nil {
		slog.WarnContext(ctx, "agent task replay failed")
		return
	}
	task, err := h.operations.RefreshCustomMetrics(ctx, nodeID)
	if err != nil {
		slog.WarnContext(ctx, "agent custom metric refresh failed")
	} else if task != nil {
		if err := wrapped.WriteTask(ctx, *task); err != nil {
			return
		}
	}
	<-ctx.Done()
}

// receiveResults reads task result messages until the connection closes.
// 单条结果落库失败只告警不断连：任务保持待执行并在重连后重投，属于既有
// at-least-once 语义；断连只会放大数据库故障期的重连风暴。
func (h *AgentChannelHandler) receiveResults(
	ctx context.Context,
	nodeID string,
	conn *websocket.Conn,
) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(agentChannelReadTimeout)); err != nil {
			slog.WarnContext(ctx, "set agent read deadline failed",
				"node_id", nodeID, "error", err)
			return
		}
		var message agentChannelMessage
		if err := websocket.JSON.Receive(conn, &message); err != nil {
			return
		}
		if identity, ok := middleware.AgentIdentity(ctx); ok && h.verifyIdentity != nil {
			if err := h.verifyIdentity(ctx, identity); err != nil {
				return
			}
		}
		switch {
		case message.Type == agentTaskResultMessage && message.Result != nil:
			if err := h.operations.CompleteAgentTask(ctx, *message.Result); err != nil {
				slog.WarnContext(ctx, "record agent task result failed; task stays pending for redelivery",
					"node_id", nodeID, "task_id", message.Result.TaskID, "error", err)
			}
		default:
			// ping 的 pong 应答或其他帧：收到即证明链路活跃。
		}
	}
}

type agentChannelMessage struct {
	Type   string                 `json:"type"`
	Result *model.AgentTaskResult `json:"result,omitempty"`
}

// writeLoop sends queued task messages and periodic keepalives. Every send
// carries a write deadline so a stalled TCP peer cannot hold the pump forever.
func writeLoop(ctx context.Context, conn *agentWebSocketConn) {
	ping := time.NewTicker(agentChannelPingEvery)
	defer ping.Stop()
	authority := time.NewTicker(agentIdentityCheckEvery)
	defer authority.Stop()
	for {
		select {
		case message, ok := <-conn.writeCh:
			if !ok {
				return
			}
			if conn.authorize != nil {
				if err := conn.authorize(ctx); err != nil {
					return
				}
			}
			if err := sendAgentFrame(conn.conn, message); err != nil {
				return
			}
		case <-authority.C:
			if conn.authorize != nil {
				if err := conn.authorize(ctx); err != nil {
					return
				}
			}
		case <-ping.C:
			if err := sendAgentFrame(conn.conn, []byte(`{"type":"ping"}`)); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func sendAgentFrame(conn *websocket.Conn, payload []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(agentChannelWriteTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if err := websocket.Message.Send(conn, string(payload)); err != nil {
		return fmt.Errorf("send agent frame: %w", err)
	}
	return nil
}

func agentChannelHandshake(config *websocket.Config, _ *http.Request) error {
	config.Origin = nil
	return nil
}

// EnqueueAuthorizedKeyTaskRequest is the JSON payload for enqueueing a key
// distribution task.
type EnqueueAuthorizedKeyTaskRequest struct {
	PublicKey string `json:"public_key"`
}

// UnmarshalAuthorizedKeyTaskRequest parses an enqueue payload.
func UnmarshalAuthorizedKeyTaskRequest(raw []byte) (string, error) {
	var request EnqueueAuthorizedKeyTaskRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return "", err
	}
	if request.PublicKey == "" {
		return "", errors.New("public_key is required")
	}
	return request.PublicKey, nil
}
