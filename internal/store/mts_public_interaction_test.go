package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

func TestWriteAndReadPublicInteractionTotals(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := setupTestMTS(t)
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Nanosecond)
	interactions := []model.PublicInteraction{
		{Event: model.PublicInteractionPageView, Surface: model.PublicSurfaceDashboard},
		{Event: model.PublicInteractionPageView, Surface: model.PublicSurfaceDashboard},
		{Event: model.PublicInteractionNodeOpen, Surface: model.PublicSurfaceNode},
	}
	if err := store.WritePublicInteractions(t.Context(), at, interactions); err != nil {
		t.Fatalf("write interactions: %v", err)
	}
	if err := store.WritePublicInteractionRejections(t.Context(), at,
		[]string{model.PublicRejectionEvent, model.PublicRejectionEvent}); err != nil {
		t.Fatalf("write rejections: %v", err)
	}
	totals, err := store.ReadPublicInteractionTotals(t.Context(), at.Add(-time.Second), at.Add(time.Second))
	if err != nil {
		t.Fatalf("read totals: %v", err)
	}
	bySeries := make(map[string]float64, len(totals))
	for _, total := range totals {
		bySeries[total.Metric+"|"+total.Event+"|"+total.Surface+"|"+total.Reason] = total.Total
	}
	expected := map[string]float64{
		model.PublicInteractionTotalMetric + "|" + model.PublicInteractionPageView + "|" + model.PublicSurfaceDashboard + "|": 2,
		model.PublicInteractionTotalMetric + "|" + model.PublicInteractionNodeOpen + "|" + model.PublicSurfaceNode + "|":      1,
		model.PublicInteractionRejectedMetric + "|||" + model.PublicRejectionEvent:                                            2,
	}
	for series, want := range expected {
		if got := bySeries[series]; got != want {
			t.Fatalf("total for %s = %v, want %v (all series: %#v)", series, got, want, bySeries)
		}
	}
	var rows int
	if err := sqliteStore.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM public_telemetry_policy").Scan(&rows); err != nil {
		t.Fatalf("count policy rows: %v", err)
	}
	if expectedRows := 1; rows != expectedRows {
		t.Fatalf("policy rows = %d, want %d: samples must never be stored in SQLite", rows, expectedRows)
	}
}

func TestPublicInteractionWritesRejectInvalidInput(t *testing.T) {
	store := setupTestMTS(t)
	at := time.Now().UTC()
	cases := map[string]func() error{
		"zero timestamp": func() error {
			return store.WritePublicInteractions(t.Context(), time.Time{}, []model.PublicInteraction{
				{Event: model.PublicInteractionPageView, Surface: model.PublicSurfaceDashboard}})
		},
		"empty batch": func() error { return store.WritePublicInteractions(t.Context(), at, nil) },
		"unknown event": func() error {
			return store.WritePublicInteractions(t.Context(), at, []model.PublicInteraction{{Event: "click", Surface: model.PublicSurfaceDashboard}})
		},
		"unknown surface": func() error {
			return store.WritePublicInteractions(t.Context(), at, []model.PublicInteraction{{Event: model.PublicInteractionPageView, Surface: "node/1"}})
		},
		"oversized batch": func() error {
			return store.WritePublicInteractions(t.Context(), at, make([]model.PublicInteraction, maximumPublicInteractionBatch+1))
		},
		"unknown rejection reason": func() error { return store.WritePublicInteractionRejections(t.Context(), at, []string{"other"}) },
		"empty reasons":            func() error { return store.WritePublicInteractionRejections(t.Context(), at, nil) },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, ErrPublicInteractionSampleRejected) {
				t.Fatalf("error = %v, want %v", err, ErrPublicInteractionSampleRejected)
			}
		})
	}
}

func TestReadPublicInteractionTotalsRejectsInvalidRangeAndClosedStore(t *testing.T) {
	store := setupTestMTS(t)
	at := time.Now().UTC()
	if _, err := store.ReadPublicInteractionTotals(t.Context(), at, at); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("error = %v, want %v", err, metricquery.ErrInvalid)
	}
	if _, err := store.ReadPublicInteractionTotals(t.Context(), time.Time{}, at); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("error = %v, want %v", err, metricquery.ErrInvalid)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := store.WritePublicInteractions(t.Context(), at, []model.PublicInteraction{
		{Event: model.PublicInteractionPageView, Surface: model.PublicSurfaceDashboard}}); err == nil {
		t.Fatal("expected write error on a closed store")
	}
	if _, err := store.ReadPublicInteractionTotals(t.Context(), at.Add(-time.Second), at.Add(time.Second)); err == nil {
		t.Fatal("expected read error on a closed store")
	}
}
