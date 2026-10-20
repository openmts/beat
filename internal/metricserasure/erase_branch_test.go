package metricserasure

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestRemainingMeasurementsResumesFromCursor(t *testing.T) {
	cases := []struct {
		name     string
		cursor   string
		expected []string
	}{
		{name: "empty cursor", cursor: "", expected: []string{"a", "b", "c"}},
		{name: "middle cursor", cursor: "b", expected: []string{"c"}},
		{name: "last cursor", cursor: "c", expected: []string{}},
		{name: "unknown cursor", cursor: "z", expected: []string{"a", "b", "c"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			remaining := remainingMeasurements([]string{"a", "b", "c"}, test.cursor)
			if len(remaining) != len(test.expected) {
				t.Fatalf("remaining = %v", remaining)
			}
			for index, value := range test.expected {
				if remaining[index] != value {
					t.Fatalf("remaining = %v", remaining)
				}
			}
		})
	}
}

func TestErasureTargetSelectsScopeIdentifiers(t *testing.T) {
	cases := []struct {
		name         string
		targetType   string
		expectedNode string
		expectedTask string
	}{
		{name: "node", targetType: model.MetricErasureTargetNode, expectedNode: "target-1"},
		{name: "network task", targetType: model.MetricErasureTargetNetworkTask, expectedTask: "target-1"},
		{name: "fleet", targetType: model.MetricErasureTargetNone},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			nodeID, taskID := erasureTarget(model.MetricErasureJob{TargetType: test.targetType, TargetID: "target-1"})
			if nodeID != test.expectedNode || taskID != test.expectedTask {
				t.Fatalf("target = %q/%q", nodeID, taskID)
			}
		})
	}
}

func TestClassifyEraseErrorMapsKnownFailures(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "inventory", err: store.ErrMetricErasureInventory, expected: model.MetricErasureErrorInventory},
		{name: "canceled", err: context.Canceled, expected: model.MetricErasureErrorLock},
		{name: "deadline", err: context.DeadlineExceeded, expected: model.MetricErasureErrorLock},
		{name: "invalid", err: model.ErrMetricErasureInvalid, expected: model.MetricErasureErrorInternal},
		{name: "storage", err: errors.New("mts unavailable"), expected: model.MetricErasureErrorMTS},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if code := classifyEraseError(test.err); code != test.expected {
				t.Fatalf("code = %s", code)
			}
		})
	}
	if code := erasureErrorCode(errors.New("plain")); code != model.MetricErasureErrorInternal {
		t.Fatalf("plain error code = %s", code)
	}
	if code := erasureErrorCode(&codedError{code: model.MetricErasureErrorFinalize, err: errors.New("coded")}); code != model.MetricErasureErrorFinalize {
		t.Fatalf("coded error code = %s", code)
	}
}

func TestEraseFailureKeepsCodedErrorExtractable(t *testing.T) {
	fixture := newErasureFixture(t)
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	if _, err := fixture.jobs.Claim(t.Context(), "worker-1", model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	failure := fixture.service.fail(t.Context(), job, model.MetricErasureErrorFinalize)
	if code := erasureErrorCode(failure); code != model.MetricErasureErrorFinalize {
		t.Fatalf("logged error code = %s", code)
	}
	if !strings.Contains(failure.Error(), model.MetricErasureErrorFinalize) {
		t.Fatalf("failure = %v", failure)
	}
}

func TestEraseFailureReportsLedgerFailure(t *testing.T) {
	fixture := newErasureFixture(t)
	job := fixture.create(t, model.MetricErasureDeleteNode, fixture.nodeID, "erasure-node")
	if err := fixture.sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	failure := fixture.service.fail(t.Context(), job, model.MetricErasureErrorInternal)
	if code := erasureErrorCode(failure); code != model.MetricErasureErrorInternal {
		t.Fatalf("logged error code = %s", code)
	}
	if !strings.Contains(failure.Error(), "fail metric erasure job") {
		t.Fatalf("failure = %v", failure)
	}
}
