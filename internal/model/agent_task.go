package model

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	AgentTaskAuthorizedKey = "authorized_key"

	AgentTaskStatusPending = "pending"
	AgentTaskStatusDone    = "done"
	AgentTaskStatusFailed  = "failed"
)

var ErrAgentTaskInvalidPayload = errors.New("invalid agent task payload")

type AgentTask struct {
	ID          string     `json:"id"`
	NodeID      string     `json:"node_id"`
	Type        string     `json:"type"`
	Payload     string     `json:"payload"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type AgentTaskResult struct {
	TaskID  string `json:"task_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// AuthorizedKeyPayload is the payload for an authorized_key agent task.
type AuthorizedKeyPayload struct {
	PublicKey string `json:"public_key"`
}

// DecodeAuthorizedKeyPayload extracts the public key from a task payload.
func DecodeAuthorizedKeyPayload(payload string) (*AuthorizedKeyPayload, error) {
	var decoded AuthorizedKeyPayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil, err
	}
	if decoded.PublicKey == "" {
		return nil, ErrAgentTaskInvalidPayload
	}
	return &decoded, nil
}

// EncodeAuthorizedKeyPayload builds a task payload for a public key.
func EncodeAuthorizedKeyPayload(publicKey string) (string, error) {
	decoded, err := json.Marshal(AuthorizedKeyPayload{PublicKey: publicKey})
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
