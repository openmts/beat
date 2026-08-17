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
	default:
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "unsupported task type " + task.Type}
	}
}

func (e *MultiTaskExecutor) executeCustomMetricRegister(task model.AgentTask) model.AgentTaskResult {
	payload, err := model.DecodeCustomMetricRegisterPayload(task.Payload)
	if err != nil {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "invalid register payload"}
	}
	e.registry.Replace(payload.Metrics)
	return model.AgentTaskResult{TaskID: task.ID, Success: true}
}
