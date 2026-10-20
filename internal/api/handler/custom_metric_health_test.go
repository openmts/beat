package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type fakeCustomSampleSource struct {
	evidences map[string]*store.CustomMetricEvidence
	errs      map[string]error
}

func (f *fakeCustomSampleSource) QueryCustomSample(_ context.Context, nodeID, metric string) (*store.CustomMetricEvidence, error) {
	key := nodeID + "|" + metric
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	return f.evidences[key], nil
}

func setupCustomMetricHealthHandler(t *testing.T, samples customSampleSource) (*CustomMetricHandler, *store.CustomMetricStore, *store.SQLiteStore) {
	t.Helper()
	h, metricStore, _, sqliteStore := setupCustomMetricHandler(t)
	if samples != nil {
		h = h.WithCustomSources(samples)
	}
	return h, metricStore, sqliteStore
}

func createHealthMetric(t *testing.T, metricStore *store.CustomMetricStore, interval int, enabled bool) model.CustomMetric {
	t.Helper()
	created, err := metricStore.Create(t.Context(), model.CustomMetric{
		Name: "probe_value", ShellCommand: "echo 1", IntervalSeconds: interval, Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("create custom metric: %v", err)
	}
	return *created
}

func bindHealthMetric(t *testing.T, metricStore *store.CustomMetricStore, sqliteStore *store.SQLiteStore, metricID string) string {
	t.Helper()
	node, err := store.NewNodeStore(sqliteStore.DB).UpsertNode(t.Context(), "health-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if err := metricStore.SetNodeBindings(t.Context(), node.ID, []string{metricID}); err != nil {
		t.Fatalf("bind metric: %v", err)
	}
	return node.ID
}

func healthRequest(t *testing.T, h *CustomMetricHandler, metricID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/custom-metrics/"+metricID+"/health", nil)
	req.SetPathValue("id", metricID)
	w := httptest.NewRecorder()
	h.HandleHealth(w, req)
	return w
}

func freshEvidence(source model.CustomSampleMetadata, at time.Time, value float64) *store.CustomMetricEvidence {
	return &store.CustomMetricEvidence{Value: value, ReportAt: at, Source: source}
}

func TestCustomMetricHealthStatesAndEvidence(t *testing.T) {
	t.Run("ok reports value and provenance", func(t *testing.T) {
		h, metricStore, sqliteStore := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		metric := createHealthMetric(t, metricStore, 30, true)
		nodeID := bindHealthMetric(t, metricStore, sqliteStore, metric.ID)
		now := time.Now().UTC()
		source := model.CustomSampleMetadata{SampleID: "11111111-1111-4111-8111-111111111111",
			CapturedAt: now.Add(-5 * time.Second), CadenceMS: 30_000, DefinitionVersion: "v1"}
		h.samples = &fakeCustomSampleSource{evidences: map[string]*store.CustomMetricEvidence{
			nodeID + "|custom_probe_value": freshEvidence(source, now.Add(-5*time.Second), 0.42),
		}}
		w := healthRequest(t, h, metric.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		body := decodeData[customMetricHealthResponse](t, w.Body.Bytes())
		if body.Metric.Name != "probe_value" || body.Metric.StaleAfterSeconds != 90 {
			t.Fatalf("metric = %#v", body.Metric)
		}
		if len(body.Nodes) != 1 {
			t.Fatalf("nodes = %#v", body.Nodes)
		}
		node := body.Nodes[0]
		if node.State != customMetricHealthStateOK || node.SampleID != source.SampleID ||
			node.CadenceMS != 30_000 || node.DefinitionVersion != "v1" || node.Value == nil || *node.Value != 0.42 {
			t.Fatalf("node = %#v", node)
		}
	})

	t.Run("stale sample is reported without discarding evidence", func(t *testing.T) {
		h, metricStore, sqliteStore := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		metric := createHealthMetric(t, metricStore, 30, true)
		nodeID := bindHealthMetric(t, metricStore, sqliteStore, metric.ID)
		at := time.Now().UTC().Add(-30 * time.Minute)
		h.samples = &fakeCustomSampleSource{evidences: map[string]*store.CustomMetricEvidence{
			nodeID + "|custom_probe_value": freshEvidence(model.CustomSampleMetadata{CapturedAt: at}, at, 3),
		}}
		body := decodeData[customMetricHealthResponse](t, healthRequest(t, h, metric.ID).Body.Bytes())
		if body.Nodes[0].State != customMetricHealthStateStale || body.Nodes[0].AgeSeconds < 1_700 {
			t.Fatalf("node = %#v", body.Nodes[0])
		}
	})

	t.Run("missing and unverified samples are distinguished", func(t *testing.T) {
		h, metricStore, sqliteStore := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		metric := createHealthMetric(t, metricStore, 30, true)
		nodeID := bindHealthMetric(t, metricStore, sqliteStore, metric.ID)
		body := decodeData[customMetricHealthResponse](t, healthRequest(t, h, metric.ID).Body.Bytes())
		if body.Nodes[0].State != customMetricHealthStateMissing {
			t.Fatalf("missing node = %#v", body.Nodes[0])
		}
		h.samples = &fakeCustomSampleSource{errs: map[string]error{
			nodeID + "|custom_probe_value": store.ErrCustomSampleSourceUnavailable,
		}}
		body = decodeData[customMetricHealthResponse](t, healthRequest(t, h, metric.ID).Body.Bytes())
		if body.Nodes[0].State != customMetricHealthStateUnverified {
			t.Fatalf("unverified node = %#v", body.Nodes[0])
		}
	})

	t.Run("disabled metric reports disabled", func(t *testing.T) {
		h, metricStore, sqliteStore := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		metric := createHealthMetric(t, metricStore, 30, false)
		_ = bindHealthMetric(t, metricStore, sqliteStore, metric.ID)
		body := decodeData[customMetricHealthResponse](t, healthRequest(t, h, metric.ID).Body.Bytes())
		if body.Nodes[0].State != customMetricHealthStateDisabled {
			t.Fatalf("node = %#v", body.Nodes[0])
		}
	})
}

func TestCustomMetricHealthFailures(t *testing.T) {
	t.Run("unbound metric returns empty node list", func(t *testing.T) {
		h, metricStore, _ := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		metric := createHealthMetric(t, metricStore, 30, true)
		w := healthRequest(t, h, metric.ID)
		body := decodeData[customMetricHealthResponse](t, w.Body.Bytes())
		if w.Code != http.StatusOK || len(body.Nodes) != 0 {
			t.Fatalf("status = %d nodes = %#v", w.Code, body.Nodes)
		}
	})

	t.Run("unknown metric returns 404", func(t *testing.T) {
		h, _, _ := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		if w := healthRequest(t, h, "missing-metric"); w.Code != http.StatusNotFound {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("missing source returns 503", func(t *testing.T) {
		h, metricStore, _ := setupCustomMetricHealthHandler(t, nil)
		metric := createHealthMetric(t, metricStore, 30, true)
		if w := healthRequest(t, h, metric.ID); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("source failure returns 500", func(t *testing.T) {
		h, metricStore, sqliteStore := setupCustomMetricHealthHandler(t, &fakeCustomSampleSource{})
		metric := createHealthMetric(t, metricStore, 30, true)
		nodeID := bindHealthMetric(t, metricStore, sqliteStore, metric.ID)
		h.samples = &fakeCustomSampleSource{errs: map[string]error{
			nodeID + "|custom_probe_value": errors.New("mts offline"),
		}}
		if w := healthRequest(t, h, metric.ID); w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}
