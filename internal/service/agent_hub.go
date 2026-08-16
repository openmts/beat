package service

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/beat/backend/internal/model"
)

// AgentConn is an established agent WebSocket channel. Implementations must be
// safe for concurrent writes.
type AgentConn interface {
	WriteTask(context.Context, model.AgentTask) error
	Close() error
}

// AgentHub tracks online agent connections and delivers pending tasks.
type AgentHub struct {
	mu    sync.RWMutex
	conns map[string]AgentConn
}

func NewAgentHub() *AgentHub {
	return &AgentHub{conns: make(map[string]AgentConn)}
}

// Register associates a connection with a node, replacing any previous one.
func (h *AgentHub) Register(nodeID string, conn AgentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if existing, ok := h.conns[nodeID]; ok && existing != conn {
		_ = existing.Close()
	}
	h.conns[nodeID] = conn
}

// Unregister removes a connection if it is the one registered for the node.
func (h *AgentHub) Unregister(nodeID string, conn AgentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.conns[nodeID]; ok && current == conn {
		delete(h.conns, nodeID)
	}
}

// Online reports whether a node currently has an active connection.
func (h *AgentHub) Online(nodeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[nodeID]
	return ok
}

// PushTask delivers a task to the node's connection when online. It returns
// true when the task was handed off to a live connection.
func (h *AgentHub) PushTask(ctx context.Context, task model.AgentTask) bool {
	h.mu.RLock()
	conn, ok := h.conns[task.NodeID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	if err := conn.WriteTask(ctx, task); err != nil {
		_ = conn.Close()
		h.mu.Lock()
		if current, exists := h.conns[task.NodeID]; exists && current == conn {
			delete(h.conns, task.NodeID)
		}
		h.mu.Unlock()
		return false
	}
	return true
}

// TaskMessage is the wire format for tasks delivered to agents.
type TaskMessage struct {
	Type string          `json:"type"`
	Task model.AgentTask `json:"task"`
}

const taskMessageType = "task"

// NewTaskMessage builds a task delivery message.
func NewTaskMessage(task model.AgentTask) ([]byte, error) {
	return json.Marshal(TaskMessage{Type: taskMessageType, Task: task})
}
