package agent

import (
	"context"

	"github.com/beat/backend/internal/model"
)

// MultiTaskExecutor dispatches agent tasks to type-specific handlers.
type MultiTaskExecutor struct {
	authorizedKeys *AuthorizedKeyExecutor
	registry       *CustomMetricRegistry
}

func NewMultiTaskExecutor(authorizedKeys *AuthorizedKeyExecutor, registry *CustomMetricRegistry) *MultiTaskExecutor {
	return &MultiTaskExecutor{authorizedKeys: authorizedKeys, registry: registry}
}

func (e *MultiTaskExecutor) ExecuteTask(ctx context.Context, task model.AgentTask) model.AgentTaskResult {
	switch task.Type {
	case model.AgentTaskAuthorizedKey:
		if e.authorizedKeys == nil {
			return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "authorized key executor unavailable"}
		}
		return e.authorizedKeys.ExecuteTask(ctx, task)
	case model.AgentTaskCustomMetricRegister:
		return e.executeCustomMetricRegister(task)
	case model.AgentTaskCustomMetricProbe:
		return e.executeCustomMetricProbe(ctx, task)
	default:
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "unsupported task type " + task.Type}
	}
}

func (e *MultiTaskExecutor) executeCustomMetricRegister(task model.AgentTask) model.AgentTaskResult {
	if e.registry == nil || len(task.Payload) > maxCustomMetricRegisterPayload {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "custom metric registration is unavailable or oversized"}
	}
	payload, err := model.DecodeCustomMetricRegisterPayload(task.Payload)
	if err != nil {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "invalid register payload"}
	}
	if err := e.registry.replace(payload.Metrics, task.CreatedAt, true); err != nil {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: err.Error()}
	}
	return model.AgentTaskResult{TaskID: task.ID, Success: true}
}

// executeCustomMetricProbe 在节点上跑一次指标命令，把有界的原始输出当作诊断证据回传。
// 试运行不改动本地注册表，成功与否只反映这条命令此刻在该节点上的执行结果。
func (e *MultiTaskExecutor) executeCustomMetricProbe(ctx context.Context, task model.AgentTask) model.AgentTaskResult {
	payload, err := model.DecodeCustomMetricProbePayload(task.Payload)
	if err != nil {
		return model.AgentTaskResult{TaskID: task.ID, Error: "invalid probe payload"}
	}
	_, raw, err := runCustomMetricCommand(ctx, payload.ShellCommand)
	result := model.AgentTaskResult{TaskID: task.ID, Output: model.TruncateAgentTaskOutput(raw)}
	if err != nil {
		result.Error = model.TruncateAgentTaskFailure(err.Error())
		return result
	}
	result.Success = true
	return result
}
