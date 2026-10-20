package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentoperations"
)

func TestOperationsRemainOptionalAndValidateTheirClient(t *testing.T) {
	if runner, err := newOperations(agent.Config{}, "unused"); err != nil || runner != nil {
		t.Fatal("legacy config enabled operations")
	}
	config := agent.Config{Operations: &agentoperations.Config{Enabled: true}}
	if _, err := newOperations(config, "unused"); err == nil {
		t.Fatal("invalid operation client accepted")
	}
	config.ServerURL = "http://127.0.0.1:1"
	config.AgentToken = "fixture-token"
	if _, err := newOperations(config, "unused"); err == nil {
		t.Fatal("invalid enabled policy accepted")
	}
}

func TestCanceledAgentInitializesAndClosesEnabledOperations(t *testing.T) {
	directory := t.TempDir()
	config := agent.Config{ServerURL: "http://127.0.0.1:1", AgentToken: "fixture-token", NodeName: "fixture", AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second, Operations: &agentoperations.Config{Enabled: true, StateDir: filepath.Join(directory, "operations")}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runAgentServices(ctx, config, filepath.Join(directory, "agent.json")); err != nil {
		t.Fatal(err)
	}
	reopened, err := newOperations(config, filepath.Join(directory, "agent.json"))
	if err != nil {
		t.Fatal("operations lease was leaked")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeComponentsCancelTogetherAndEventsAreBounded(t *testing.T) {
	failure := errors.New("component unavailable")
	waiting := func(ctx context.Context) error { <-ctx.Done(); return nil }
	if err := runComponents(t.Context(), func(context.Context) error { return failure }, waiting, waiting); !errors.Is(err, failure) {
		t.Fatal("component failure was lost")
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	logOperationEvent(agentoperations.Event{AttemptID: "attempt", State: "succeeded", OutputBytes: 17})
	if !strings.Contains(output.String(), `"output_bytes":17`) {
		t.Fatal("metadata event missing")
	}
}
