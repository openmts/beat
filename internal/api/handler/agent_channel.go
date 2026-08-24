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
	// 读超时为 ping 周期的 3 倍；agent 收到 ping 会回复 pong 续期。
	agentChannelReadTimeout  = 90 * time.Second
	agentChannelWriteTimeout = 30 * time.Second
)

// AgentChannelOperations exposes the pieces the agent channel needs.
type AgentChannelOperations interface {
	EnqueueAuthorizedKeyTask(context.Context, string, string) (*model.AgentTask, error)
	RegisterAgent(context.Context, string) ([]model.AgentTask, error)
	CompleteAgentTask(context.Context, string, string, string) error
	// RefreshCustomMetrics enqueues a registration task so the agent rebuilds
	// its local custom metric registry on every (re)connect.
	RefreshCustomMetrics(context.Context, string) (*model.AgentTask, error)
}

type AgentChannelHandler struct {
	operations AgentChannelOperations
	hub        *service.AgentHub
}

func NewAgentChannelHandler(operations AgentChannelOperations, hub *service.AgentHub) *AgentChannelHandler {
	return &AgentChannelHandler{operations: operations, hub: hub}
}

// agentWebSocketConn adapts a websocket connection for the AgentHub and pumps
// outbound task messages from a channel.
type agentWebSocketConn struct {
	conn    *websocket.Conn
	writeCh chan []byte
}

func (c *agentWebSocketConn) WriteTask(ctx context.Context, task model.AgentTask) error {
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
	wrapped := &agentWebSocketConn{conn: conn, writeCh: make(chan []byte, 16)}
	h.hub.Register(nodeID, wrapped)
	defer h.hub.Unregister(nodeID, wrapped)

	pending, err := h.operations.RegisterAgent(ctx, nodeID)
	if err != nil {
		slog.WarnContext(ctx, "agent channel register failed",
			"node_id", nodeID, "error", err)
		_ = conn.Close()
		return
	}
	for _, task := range pending {
		if err := wrapped.WriteTask(ctx, task); err != nil {
			return
		}
	}
	// Re-sync the custom metric registry on every connect so agents that
	// restarted (or lost the earlier registration task) rebuild their state.
	task, refreshErr := h.operations.RefreshCustomMetrics(ctx, nodeID)
	if refreshErr != nil {
		slog.WarnContext(ctx, "agent channel refresh custom metrics failed",
			"node_id", nodeID, "error", refreshErr)
	} else if task != nil {
		if err := wrapped.WriteTask(ctx, *task); err != nil {
			return
		}
	}

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		writeLoop(ctx, wrapped)
	}()

	h.receiveResults(ctx, nodeID, conn)

	_ = conn.Close()
	<-writeDone
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
		switch {
		case message.Type == agentTaskResultMessage && message.Result != nil:
			status := model.AgentTaskStatusFailed
			if message.Result.Success {
				status = model.AgentTaskStatusDone
			}
			if err := h.operations.CompleteAgentTask(
				ctx, message.Result.TaskID, status, message.Result.Error,
			); err != nil {
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
	for {
		select {
		case message, ok := <-conn.writeCh:
			if !ok {
				return
			}
			if err := sendAgentFrame(conn.conn, message); err != nil {
				return
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
