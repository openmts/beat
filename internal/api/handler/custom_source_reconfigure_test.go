package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type reconfigureCollector struct{}

func (reconfigureCollector) Collect(context.Context) (model.NodeMetrics, error) {
	return model.NodeMetrics{CPU: 25, CapturedAt: time.Now()}, nil
}

func TestPendingCustomReportRecoversPromptlyAfterDefinitionChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the Unix collector command")
	}
	fixture, definition := customSourceReportFixture(t)
	registry := agent.NewCustomMetricRegistry()
	if err := registry.Replace([]model.CustomMetric{definition}); err != nil {
		t.Fatal(err)
	}
	state := &customReconfigureState{fixture: fixture, definition: definition, registry: registry, done: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(state.serve))
	t.Cleanup(server.Close)
	reporter, err := agent.NewHTTPReporter(agent.Config{ServerURL: server.URL, AgentToken: fixture.token, NodeName: fixture.node.Name, AdvertisedHost: fixture.report.Host, SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	runner := agent.NewRunner(agent.RunnerOptions{Interval: 10 * time.Millisecond, Collector: reconfigureCollector{}, Reporter: reporter, CustomSampler: agent.NewCustomMetricCollector(registry)})
	finished := make(chan error, 1)
	go func() { finished <- runner.Run(ctx) }()
	select {
	case <-state.done:
	case <-ctx.Done():
		t.Error("superseded pending sample did not recover promptly")
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.err != nil || state.rejected == "" || state.repeated || !state.recovered {
		t.Fatalf("reconfiguration state: rejected=%t repeated=%t recovered=%t err=%v", state.rejected != "", state.repeated, state.recovered, state.err)
	}
}

type customReconfigureState struct {
	mu                  sync.Mutex
	fixture             agentReportFixture
	definition          model.CustomMetric
	registry            *agent.CustomMetricRegistry
	rejected            string
	repeated, recovered bool
	err                 error
	done                chan struct{}
}

func (state *customReconfigureState) serve(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	defer state.mu.Unlock()
	body, err := io.ReadAll(io.LimitReader(r.Body, model.MaximumAgentReportBytes+1))
	if err != nil {
		state.err = err
		return
	}
	var report model.AgentReport
	if err := json.Unmarshal(body, &report); err != nil {
		state.err = err
		return
	}
	if state.rejected != "" && report.SampleID == state.rejected {
		state.repeated = true
	}
	if len(report.CustomMetrics) > 0 && state.rejected == "" {
		state.rejected = report.SampleID
		state.definition.ShellCommand, state.definition.IntervalSeconds = "printf 9", 10
		changed, err := store.NewCustomMetricStore(state.fixture.sqlite.DB).UpdateWithRegistration(r.Context(), state.definition.ID, state.definition)
		if err != nil {
			state.err = err
			return
		}
		state.definition = *changed.Metric
		if err := state.registry.Replace([]model.CustomMetric{state.definition}); err != nil {
			state.err = err
			return
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	response := httptest.NewRecorder()
	middleware.AgentReportAuth(state.fixture.handler.nodeStore, "", nil)(http.HandlerFunc(state.fixture.handler.HandleNodeReport)).ServeHTTP(response, r)
	w.WriteHeader(response.Code)
	if _, err := w.Write(response.Body.Bytes()); err != nil {
		state.err = err
		return
	}
	if response.Code == 201 && state.rejected != "" && report.CustomMetrics["queue"] == 9 && !state.recovered {
		state.recovered = true
		close(state.done)
	}
}
