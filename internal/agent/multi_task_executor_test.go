package agent

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMultiTaskExecutorCustomMetricRegister(t *testing.T) {
	registry := NewCustomMetricRegistry()
	executor := NewMultiTaskExecutor(nil, registry)
	payload, err := model.EncodeCustomMetricRegisterPayload([]model.CustomMetric{
		{Name: "latency", ShellCommand: "echo 5", IntervalSeconds: 15},
	})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	result := executor.ExecuteTask(context.Background(), model.AgentTask{
		ID: "t1", Type: model.AgentTaskCustomMetricRegister, Payload: payload,
	})
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}
	all := registry.All()
	if len(all) != 1 || all[0].Name != "latency" {
		t.Fatalf("registry = %#v, want one latency metric", all)
	}
}

func TestMultiTaskExecutorCustomMetricRegisterInvalid(t *testing.T) {
	executor := NewMultiTaskExecutor(nil, NewCustomMetricRegistry())
	for _, payload := range []string{"", `{"metrics":null}`, "not-json"} {
		result := executor.ExecuteTask(context.Background(), model.AgentTask{
			ID: "t1", Type: model.AgentTaskCustomMetricRegister, Payload: payload,
		})
		if result.Success {
			t.Fatalf("payload %q should fail", payload)
		}
	}
}

func TestMultiTaskExecutorUnsupportedType(t *testing.T) {
	executor := NewMultiTaskExecutor(nil, NewCustomMetricRegistry())
	result := executor.ExecuteTask(context.Background(), model.AgentTask{
		ID: "t1", Type: "bogus", Payload: "{}",
	})
	if result.Success || result.Error == "" {
		t.Fatalf("result = %#v, want failure", result)
	}
}

func TestMultiTaskExecutorAuthorizedKeyUnavailable(t *testing.T) {
	executor := NewMultiTaskExecutor(nil, NewCustomMetricRegistry())
	result := executor.ExecuteTask(context.Background(), model.AgentTask{
		ID: "t1", Type: model.AgentTaskAuthorizedKey, Payload: `{"public_key":"ssh-ed25519 aaa"}`,
	})
	if result.Success {
		t.Fatalf("result = %#v, want failure when executor unavailable", result)
	}
}
