package agentdiagnostics

import "github.com/beat/backend/internal/model"

// Status 是 Agent 诊断运行时的只读观测面。
type Status struct {
	Enabled   bool   `json:"enabled"`
	Ready     bool   `json:"ready"`
	Active    int    `json:"active"`
	Completed int64  `json:"completed"`
	Failed    int64  `json:"failed"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (runner *Runner) Status() Status {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return Status{Enabled: runner.settings.config.Enabled, Ready: runner.lastError == "", Active: len(runner.active),
		Completed: runner.completed, Failed: runner.failed, ErrorCode: runner.lastError}
}

// enter 与 leave 维护活动作业集合；两者在执行路径上成对调用。
func (runner *Runner) enter(jobID string) {
	runner.mu.Lock()
	runner.active[jobID] = struct{}{}
	runner.mu.Unlock()
}

func (runner *Runner) leave(jobID string) {
	runner.mu.Lock()
	delete(runner.active, jobID)
	runner.mu.Unlock()
}

// count 记录一次已确认终态的作业；partial 与 succeeded 都算成功结束。
func (runner *Runner) count(state string) {
	runner.mu.Lock()
	if state == model.DiagnosticStateSucceeded || state == model.DiagnosticStatePartial {
		runner.completed++
	} else {
		runner.failed++
	}
	runner.mu.Unlock()
}
