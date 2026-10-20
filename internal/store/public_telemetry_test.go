package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestPublicTelemetryStoreDefaultsAndUpdate(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	policy, err := store.Get(t.Context())
	if err != nil {
		t.Fatalf("get default policy: %v", err)
	}
	if policy.Enabled || policy.SampleCeiling != 1000 || policy.RetentionDays != 30 || policy.Revision != 1 {
		t.Fatalf("default policy = %#v", policy)
	}
	if expected, actual := model.PublicInteractionEvents(), policy.AllowedEvents; len(actual) != len(expected) {
		t.Fatalf("default allowed events = %v, want %v", actual, expected)
	}

	policy.Enabled = true
	policy.AllowedEvents = []string{model.PublicInteractionPageView}
	policy.SampleCeiling = 250
	policy.RetentionDays = 45
	policy.Revision = 2
	policy.UpdatedBy = "owner"
	updated, err := store.Update(t.Context(), policy)
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if updated.UpdatedAt.IsZero() {
		t.Fatalf("updated policy = %#v", updated)
	}
	persisted, err := store.Get(t.Context())
	if err != nil {
		t.Fatalf("get updated policy: %v", err)
	}
	if !persisted.Enabled || persisted.SampleCeiling != 250 || persisted.RetentionDays != 45 || persisted.Revision != 2 ||
		persisted.UpdatedBy != "owner" || len(persisted.AllowedEvents) != 1 {
		t.Fatalf("persisted policy = %#v", persisted)
	}
	for _, key := range model.PublicInteractionMetrics() {
		var retention int
		if err := sqliteStore.DB.QueryRowContext(t.Context(),
			"SELECT retention_days FROM metric_definitions WHERE metric_key = ?", key).Scan(&retention); err != nil {
			t.Fatalf("read retention for %s: %v", key, err)
		}
		if expected, actual := 45, retention; expected != actual {
			t.Fatalf("retention override for %s = %d, want %d", key, actual, expected)
		}
	}
}

func TestPublicTelemetryStoreRequiresThePolicyRow(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	if _, err := sqliteStore.DB.ExecContext(t.Context(), "DELETE FROM public_telemetry_policy"); err != nil {
		t.Fatalf("delete policy row: %v", err)
	}
	if _, err := store.Get(t.Context()); err == nil {
		t.Fatal("expected read error without a policy row")
	}
	policy := model.DefaultPublicTelemetryPolicy()
	policy.Revision = 2
	if _, err := store.Update(t.Context(), policy); err == nil {
		t.Fatal("expected update error without a policy row")
	}
}

func TestPublicTelemetryStoreAuditsSameLengthEventChange(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	first := model.DefaultPublicTelemetryPolicy()
	first.Revision = 2
	first.AllowedEvents = []string{model.PublicInteractionPageView}
	if _, err := store.Update(t.Context(), first); err != nil {
		t.Fatalf("first update: %v", err)
	}
	second := first
	second.Revision = 3
	second.AllowedEvents = []string{model.PublicInteractionNodeOpen}
	updated, err := store.Update(t.Context(), second)
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if expected, actual := []string{model.PublicInteractionNodeOpen}, updated.AllowedEvents; len(actual) != 1 || actual[0] != expected[0] {
		t.Fatalf("allowed events = %v, want %v", actual, expected)
	}
}

func TestPublicTelemetryStoreRejectsCorruptEventPayload(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	if _, err := sqliteStore.DB.ExecContext(t.Context(),
		"UPDATE public_telemetry_policy SET allowed_events_json = '{'"); err != nil {
		t.Fatalf("corrupt policy row: %v", err)
	}
	if _, err := store.Get(t.Context()); err == nil {
		t.Fatal("expected decode error for a corrupt event list")
	}
}

func TestPublicTelemetryStoreRejectsStaleRevision(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	stale := model.DefaultPublicTelemetryPolicy()
	stale.Enabled = true
	if _, err := store.Update(t.Context(), stale); !errors.Is(err, ErrPublicTelemetryConflict) {
		t.Fatalf("update error = %v, want %v", err, ErrPublicTelemetryConflict)
	}
}

func TestPublicTelemetryStoreRejectsInvalidPolicy(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	invalid := model.DefaultPublicTelemetryPolicy()
	invalid.Revision = 2
	invalid.AllowedEvents = []string{"click"}
	if _, err := store.Update(t.Context(), invalid); !errors.Is(err, model.ErrPublicTelemetryPolicy) {
		t.Fatalf("update error = %v, want %v", err, model.ErrPublicTelemetryPolicy)
	}
	persisted, err := store.Get(t.Context())
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if persisted.Enabled || persisted.SampleCeiling != 1000 {
		t.Fatalf("rejected update must not change the stored policy: %#v", persisted)
	}
}

func TestPublicTelemetryStoreErrorsOnClosedDatabase(t *testing.T) {
	sqliteStore := setupTestDB(t)
	store := NewPublicTelemetryStore(sqliteStore.DB)
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if _, err := store.Get(t.Context()); err == nil {
		t.Fatal("expected get error")
	}
	closed := model.DefaultPublicTelemetryPolicy()
	closed.Revision = 2
	if _, err := store.Update(t.Context(), closed); err == nil {
		t.Fatal("expected update error")
	}
}
