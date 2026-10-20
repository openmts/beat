package api

import (
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/store"
)

func reportMetricsFixture(t *testing.T) *store.MTSStore {
	t.Helper()
	metrics, err := store.NewMTSStore(filepath.Join(t.TempDir(), "metrics"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := metrics.Close(); err != nil {
			t.Error(err)
		}
	})
	return metrics
}

func reportMetricsFixtureOption(t *testing.T) RouterOption {
	metrics := reportMetricsFixture(t)
	return func(router *Router) { router.mtsStore = metrics }
}
