package model

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AgentTaskAuthorizedKey     = "authorized_key"
	AgentTaskCustomMetricProbe = "custom_metric_probe"

	AgentTaskStatusPending = "pending"
	AgentTaskStatusDone    = "done"
	AgentTaskStatusFailed  = "failed"

	// 任务结果里的命令输出与失败原因都有硬上限：它们是诊断证据，不是数据通道。
	MaximumAgentTaskOutputBytes  = 4 << 10
	MaximumAgentTaskFailureBytes = 512
	// 试运行载荷只带一次命令，长度与自定义指标本身的命令上限一致。
	MaximumAgentTaskProbeCommandBytes = 2048
)

var ErrAgentTaskInvalidPayload = errors.New("invalid agent task payload")

type AgentTask struct {
	ID          string     `json:"id"`
	NodeID      string     `json:"node_id"`
	Type        string     `json:"type"`
	Payload     string     `json:"payload"`
	Status      string     `json:"status"`
	Output      string     `json:"output,omitempty"`
	Failure     string     `json:"failure,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type AgentTaskResult struct {
	TaskID  string `json:"task_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	// Output 是命令输出的有界前缀，只用于人工诊断。
	Output string `json:"output,omitempty"`
}

// CustomMetricProbePayload 是「试运行」任务的载荷：一次命令与它所属的指标。
type CustomMetricProbePayload struct {
	MetricID     string `json:"metric_id"`
	ShellCommand string `json:"shell_command"`
}

// DecodeCustomMetricProbePayload 解析并校验试运行载荷，任何越界都按非法载荷拒绝。
func DecodeCustomMetricProbePayload(payload string) (*CustomMetricProbePayload, error) {
	var decoded CustomMetricProbePayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil, ErrAgentTaskInvalidPayload
	}
	if strings.TrimSpace(decoded.MetricID) == "" || len(decoded.MetricID) > 64 {
		return nil, ErrAgentTaskInvalidPayload
	}
	if strings.TrimSpace(decoded.ShellCommand) == "" || len(decoded.ShellCommand) > MaximumAgentTaskProbeCommandBytes {
		return nil, ErrAgentTaskInvalidPayload
	}
	return &decoded, nil
}

// EncodeCustomMetricProbePayload 构造试运行载荷；越界直接报非法载荷，不给上层留下半截命令。
func EncodeCustomMetricProbePayload(metricID string, shellCommand string) (string, error) {
	decoded := CustomMetricProbePayload{MetricID: metricID, ShellCommand: shellCommand}
	if strings.TrimSpace(metricID) == "" || len(metricID) > 64 || strings.TrimSpace(shellCommand) == "" || len(shellCommand) > MaximumAgentTaskProbeCommandBytes {
		return "", ErrAgentTaskInvalidPayload
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// TruncateAgentTaskOutput 把命令输出裁到有界前缀，并在多字节字符边界处收尾。
func TruncateAgentTaskOutput(value string) string {
	return truncateAgentTaskText(value, MaximumAgentTaskOutputBytes)
}

// TruncateAgentTaskFailure 把失败原因裁到有界前缀；原因会回显非法 token，同样必须限量。
func TruncateAgentTaskFailure(value string) string {
	return truncateAgentTaskText(value, MaximumAgentTaskFailureBytes)
}

func truncateAgentTaskText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	cut := value[:maximum]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
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
