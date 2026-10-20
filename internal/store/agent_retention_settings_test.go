package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestAgentRetentionStoreDefaultsToEnvironmentBehaviour(t *testing.T) {
	database := setupTestDB(t)
	store := NewAgentRetentionStore(database.DB)
	settings, err := store.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings.Configured || settings.RolloutDays != 0 || settings.ReleaseDays != 0 {
		t.Fatalf("default settings = %+v", settings)
	}
	if settings.Status.LastStatus != model.AgentRetentionStatusIdle || settings.Status.LastRunAt != nil ||
		settings.Status.LastTrigger != "" {
		t.Fatalf("default run status = %+v", settings.Status)
	}
	if settings.UpdatedAt.IsZero() {
		t.Fatal("seeded settings lost their update timestamp")
	}
}

func TestAgentRetentionStorePersistsConfigurationRunsAndReset(t *testing.T) {
	database := setupTestDB(t)
	store := NewAgentRetentionStore(database.DB)
	invalid := model.AgentRetentionSettings{RolloutDays: model.MaximumReleaseRetentionDays + 1}
	if err := store.Update(t.Context(), invalid); !errors.Is(err, model.ErrAgentRetentionInvalid) {
		t.Fatalf("update accepted an out-of-range period: %v", err)
	}
	if settings, err := store.Get(t.Context()); err != nil || settings.Configured {
		t.Fatalf("invalid update changed the row: %+v, %v", settings, err)
	}
	if err := store.Update(t.Context(), model.AgentRetentionSettings{RolloutDays: 7, ReleaseDays: 0}); err != nil {
		t.Fatal(err)
	}
	settings, err := store.Get(t.Context())
	if err != nil || !settings.Configured || settings.RolloutDays != 7 || settings.ReleaseDays != 0 {
		t.Fatalf("stored settings = %+v, err = %v", settings, err)
	}
	counts := model.AgentRetentionRunStatus{LastRollouts: 3, LastReleases: 2, LastArtifacts: 1}
	if err := store.MarkRun(t.Context(), model.AgentRetentionStatusFailed, "artifact is busy",
		model.AgentRetentionTriggerSchedule, counts); err != nil {
		t.Fatal(err)
	}
	settings, err = store.Get(t.Context())
	if err != nil || settings.Status.LastRunAt == nil {
		t.Fatalf("run ledger = %+v, err = %v", settings.Status, err)
	}
	if settings.Status.LastStatus != model.AgentRetentionStatusFailed || settings.Status.LastError != "artifact is busy" ||
		settings.Status.LastTrigger != model.AgentRetentionTriggerSchedule ||
		settings.Status.LastRollouts != 3 || settings.Status.LastReleases != 2 || settings.Status.LastArtifacts != 1 {
		t.Fatalf("run ledger = %+v", settings.Status)
	}
	if err := store.Reset(t.Context()); err != nil {
		t.Fatal(err)
	}
	settings, err = store.Get(t.Context())
	if err != nil || settings.Configured || settings.RolloutDays != 0 || settings.ReleaseDays != 0 {
		t.Fatalf("reset settings = %+v, err = %v", settings, err)
	}
	if settings.Status.LastStatus != model.AgentRetentionStatusFailed || settings.Status.LastRunAt == nil {
		t.Fatalf("reset dropped the run ledger: %+v", settings.Status)
	}
}

func TestAgentRetentionStoreReportsDatabaseFailures(t *testing.T) {
	database := setupTestDB(t)
	store := NewAgentRetentionStore(database.DB)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func() error{
		"get": func() error { _, err := store.Get(t.Context()); return err },
		"update": func() error {
			return store.Update(t.Context(), model.AgentRetentionSettings{RolloutDays: 1})
		},
		"reset": func() error { return store.Reset(t.Context()) },
		"mark": func() error {
			return store.MarkRun(t.Context(), model.AgentRetentionStatusOK, "", model.AgentRetentionTriggerManual,
				model.AgentRetentionRunStatus{})
		},
	}
	for name, call := range cases {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "agent release retention") {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}
