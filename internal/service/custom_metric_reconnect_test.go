//go:build linux

package service_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

type brokenLiveCustomMetricConnection struct{}

func (brokenLiveCustomMetricConnection) WriteTask(context.Context, model.AgentTask) error {
	return errors.New("connection closed before snapshot delivery")
}

func (brokenLiveCustomMetricConnection) Close() error { return nil }

func TestLiveCustomMetricRevocationReplaysAfterDeliveryFailure(t *testing.T) {
	fixture := newLiveCustomMetricFixture(t)
	pid := fixture.scriptPID()
	fixture.hub.Register(fixture.nodeID, brokenLiveCustomMetricConnection{})
	request := httptest.NewRequest(http.MethodDelete, "/", nil)
	request.SetPathValue("id", fixture.metric.ID)
	response := httptest.NewRecorder()
	fixture.handler.HandleDelete(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("revocation status=%d", response.Code)
	}
	pending, err := fixture.tasks.GetPendingTasks(t.Context(), fixture.nodeID)
	if err != nil || len(pending) != 1 || !strings.Contains(pending[0].Payload, `"metrics":[]`) {
		t.Fatal("failed live delivery lost the pending revocation")
	}
	waitLiveCustomMetric(t, func() bool {
		return fixture.connections.Load() >= 2 && len(fixture.registry.All()) == 0 && !liveMetricProcessRunning(pid)
	})
	waitLiveCustomMetric(t, func() bool {
		remaining, err := fixture.tasks.GetPendingTasks(t.Context(), fixture.nodeID)
		if err != nil {
			t.Fatal(err)
		}
		return len(remaining) == 0
	})
}
