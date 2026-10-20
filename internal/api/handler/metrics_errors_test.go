package handler

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestMetricsSnapshotRejectsEveryStorageReadFailure(t *testing.T) {
	for _, kind := range []string{"nodes", "settings", "catalog", "closed_after_query"} {
		t.Run(kind, func(t *testing.T) {
			h, mts := setupMetricsTest(t)
			if _, err := h.nodeStore.UpsertNode(t.Context(), "visible", "192.0.2.1", 22); err != nil {
				t.Fatal(err)
			}
			closed := setupTestDB(t)
			if err := closed.Close(); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "nodes":
				h.nodeStore = store.NewNodeStore(closed.DB)
			case "settings":
				h.settingsStore = store.NewSiteSettingsStore(closed.DB)
			case "catalog":
				h.metricCatalog = store.NewMetricCatalogStore(closed.DB)
			case "closed_after_query":
				h.WithLocationViews(closeMetricsAfterRead{mts: mts})
			}
			snapshot, err := h.BuildMetricsSnapshot(t.Context())
			if err == nil || snapshot.Value != nil {
				t.Fatal("failed read published a partial snapshot", snapshot, err)
			}
		})
	}
}

type closeMetricsAfterRead struct{ mts *store.MTSStore }

func (view closeMetricsAfterRead) PublicLocation(context.Context, string) (*model.LocationPublic, error) {
	return nil, view.mts.Close()
}
