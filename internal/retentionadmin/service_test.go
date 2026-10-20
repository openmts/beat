package retentionadmin

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/model"
)

type markCall struct {
	status  string
	reason  string
	trigger string
	record  model.AgentRetentionRunStatus
}

type fakeStore struct {
	settings    model.AgentRetentionSettings
	getErr      error
	updateErr   error
	resetErr    error
	markErr     error
	failGetFrom int
	getCalls    int
	updates     []model.AgentRetentionSettings
	resets      int
	marks       []markCall
}

func (store *fakeStore) Get(context.Context) (model.AgentRetentionSettings, error) {
	store.getCalls++
	if store.getErr != nil && (store.failGetFrom == 0 || store.getCalls >= store.failGetFrom) {
		return model.AgentRetentionSettings{}, store.getErr
	}
	return store.settings, nil
}

func (store *fakeStore) Update(_ context.Context, settings model.AgentRetentionSettings) error {
	if store.updateErr != nil {
		return store.updateErr
	}
	store.updates = append(store.updates, settings)
	store.settings.Configured = true
	store.settings.RolloutDays, store.settings.ReleaseDays = settings.RolloutDays, settings.ReleaseDays
	return nil
}

func (store *fakeStore) Reset(context.Context) error {
	if store.resetErr != nil {
		return store.resetErr
	}
	store.resets++
	store.settings.Configured = false
	store.settings.RolloutDays, store.settings.ReleaseDays = 0, 0
	return nil
}

func (store *fakeStore) MarkRun(_ context.Context, status, reason, trigger string, record model.AgentRetentionRunStatus) error {
	if store.markErr != nil {
		return store.markErr
	}
	store.marks = append(store.marks, markCall{status: status, reason: reason, trigger: trigger, record: record})
	return nil
}

type fakePruner struct {
	counts   agentrollout.RetentionCounts
	err      error
	policies []agentrollout.RetentionPolicy
}

func (pruner *fakePruner) Prune(_ context.Context, policy agentrollout.RetentionPolicy) (agentrollout.RetentionCounts, error) {
	pruner.policies = append(pruner.policies, policy)
	return pruner.counts, pruner.err
}

func TestServiceWithoutStoreIsUnavailable(t *testing.T) {
	services := map[string]*Service{
		"nil service": nil,
		"nil store":   New(nil, &fakePruner{}, agentrollout.RetentionPolicy{}),
	}
	for name, service := range services {
		if _, err := service.Get(t.Context()); err == nil {
			t.Fatalf("%s: Get accepted a missing store", name)
		}
		if _, err := service.Update(t.Context(), model.AgentRetentionSettings{}); err == nil {
			t.Fatalf("%s: Update accepted a missing store", name)
		}
		if _, err := service.Reset(t.Context()); err == nil {
			t.Fatalf("%s: Reset accepted a missing store", name)
		}
		if _, err := service.Run(t.Context()); err == nil {
			t.Fatalf("%s: Run accepted a missing store", name)
		}
		if _, err := service.Effective(t.Context()); err == nil {
			t.Fatalf("%s: Effective accepted a missing store", name)
		}
	}
}

func TestServiceRunRequiresPruner(t *testing.T) {
	service := New(&fakeStore{}, nil, agentrollout.RetentionPolicy{RolloutDays: 7})
	if _, err := service.Run(t.Context()); err == nil {
		t.Fatal("Run accepted a missing pruner")
	}
}

func TestServiceGetPublishesEnvironmentDefaults(t *testing.T) {
	store := &fakeStore{settings: model.AgentRetentionSettings{RolloutDays: 4}}
	settings, err := New(store, &fakePruner{}, agentrollout.RetentionPolicy{RolloutDays: 30}).Get(t.Context())
	if err != nil || settings.RolloutDays != 4 || settings.Environment.RolloutDays != 30 || !settings.Environment.Enabled {
		t.Fatalf("configured defaults = %+v, err = %v", settings, err)
	}
	settings, err = New(store, &fakePruner{}, agentrollout.RetentionPolicy{}).Get(t.Context())
	if err != nil || settings.Environment.Enabled || settings.Environment.RolloutDays != 0 {
		t.Fatalf("disabled defaults = %+v, err = %v", settings, err)
	}
	store.getErr = errors.New("read failed")
	if _, err := New(store, &fakePruner{}, agentrollout.RetentionPolicy{}).Get(t.Context()); err == nil {
		t.Fatal("Get ignored a store failure")
	}
}

func TestServiceUpdateValidatesBeforePersisting(t *testing.T) {
	store := &fakeStore{}
	service := New(store, &fakePruner{}, agentrollout.RetentionPolicy{RolloutDays: 30})
	invalid := model.AgentRetentionSettings{RolloutDays: model.MaximumReleaseRetentionDays + 1}
	if _, err := service.Update(t.Context(), invalid); !errors.Is(err, model.ErrAgentRetentionInvalid) {
		t.Fatalf("Update accepted an out-of-range period: %v", err)
	}
	if len(store.updates) != 0 {
		t.Fatal("invalid settings reached the store")
	}
	settings, err := service.Update(t.Context(), model.AgentRetentionSettings{RolloutDays: 7})
	if err != nil || !settings.Configured || settings.RolloutDays != 7 || settings.Environment.RolloutDays != 30 {
		t.Fatalf("Update = %+v, err = %v", settings, err)
	}
	store.updateErr = errors.New("write failed")
	if _, err := service.Update(t.Context(), model.AgentRetentionSettings{RolloutDays: 1}); err == nil {
		t.Fatal("Update ignored a store failure")
	}
}

func TestServiceResetReturnsToEnvironmentDefaults(t *testing.T) {
	store := &fakeStore{settings: model.AgentRetentionSettings{Configured: true, RolloutDays: 9}}
	service := New(store, &fakePruner{}, agentrollout.RetentionPolicy{RolloutDays: 30})
	settings, err := service.Reset(t.Context())
	if err != nil || store.resets != 1 || settings.Configured || settings.RolloutDays != 0 || settings.Environment.RolloutDays != 30 {
		t.Fatalf("Reset = %+v, resets = %d, err = %v", settings, store.resets, err)
	}
	store.resetErr = errors.New("reset failed")
	if _, err := service.Reset(t.Context()); err == nil {
		t.Fatal("Reset ignored a store failure")
	}
}

func TestServiceEffectivePrefersConfiguredSettings(t *testing.T) {
	environment := agentrollout.RetentionPolicy{RolloutDays: 30, ReleaseDays: 90}
	store := &fakeStore{}
	service := New(store, &fakePruner{}, environment)
	policy, err := service.Effective(t.Context())
	if err != nil || policy != environment {
		t.Fatalf("Effective = %+v, err = %v", policy, err)
	}
	store.settings = model.AgentRetentionSettings{Configured: true, RolloutDays: 3, ReleaseDays: 5}
	if policy, err = service.Effective(t.Context()); err != nil || policy != (agentrollout.RetentionPolicy{RolloutDays: 3, ReleaseDays: 5}) {
		t.Fatalf("configured Effective = %+v, err = %v", policy, err)
	}
	store.settings = model.AgentRetentionSettings{Configured: true, ReleaseDays: -1}
	if _, err := service.Effective(t.Context()); !errors.Is(err, model.ErrAgentRetentionInvalid) {
		t.Fatalf("Effective accepted invalid stored settings: %v", err)
	}
}

func TestServiceRunRejectsDisabledRetention(t *testing.T) {
	store, pruner := &fakeStore{}, &fakePruner{}
	service := New(store, pruner, agentrollout.RetentionPolicy{})
	if _, err := service.Run(t.Context()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Run accepted disabled retention: %v", err)
	}
	if len(pruner.policies) != 0 || len(store.marks) != 0 {
		t.Fatal("disabled Run touched the pruner or the run ledger")
	}
}

func TestServiceRunPrunesAndRecordsManualTrigger(t *testing.T) {
	store := &fakeStore{settings: model.AgentRetentionSettings{Configured: true, RolloutDays: 5}}
	pruner := &fakePruner{counts: agentrollout.RetentionCounts{Rollouts: 2, Releases: 1, Artifacts: 3}}
	settings, err := New(store, pruner, agentrollout.RetentionPolicy{}).Run(t.Context())
	if err != nil || len(pruner.policies) != 1 || pruner.policies[0].RolloutDays != 5 {
		t.Fatalf("prune policies = %+v, err = %v", pruner.policies, err)
	}
	if len(store.marks) != 1 {
		t.Fatalf("run ledger = %+v", store.marks)
	}
	mark := store.marks[0]
	if mark.status != model.AgentRetentionStatusOK || mark.reason != "" || mark.trigger != model.AgentRetentionTriggerManual {
		t.Fatalf("mark = %+v", mark)
	}
	if mark.record.LastRollouts != 2 || mark.record.LastReleases != 1 || mark.record.LastArtifacts != 3 {
		t.Fatalf("mark counts = %+v", mark.record)
	}
	if !settings.Configured || settings.RolloutDays != 5 {
		t.Fatalf("Run settings = %+v", settings)
	}
}

func TestServiceRunRecordsFailureReasonAndReturnsIt(t *testing.T) {
	pruneErr := errors.New("prune exploded")
	store := &fakeStore{settings: model.AgentRetentionSettings{Configured: true, ReleaseDays: 10}}
	service := New(store, &fakePruner{err: pruneErr}, agentrollout.RetentionPolicy{})
	if _, err := service.Run(t.Context()); !errors.Is(err, pruneErr) {
		t.Fatalf("Run hid the prune failure: %v", err)
	}
	mark := store.marks[0]
	if mark.status != model.AgentRetentionStatusFailed || mark.reason != pruneErr.Error() ||
		mark.trigger != model.AgentRetentionTriggerManual {
		t.Fatalf("failure mark = %+v", mark)
	}
}

func TestServicePruneJoinsLedgerAndReadFailures(t *testing.T) {
	pruneErr, markErr := errors.New("prune exploded"), errors.New("ledger failed")
	store := &fakeStore{settings: model.AgentRetentionSettings{Configured: true, RolloutDays: 1}, markErr: markErr}
	service := New(store, &fakePruner{err: pruneErr}, agentrollout.RetentionPolicy{})
	if _, err := service.Run(t.Context()); !errors.Is(err, pruneErr) || !errors.Is(err, markErr) {
		t.Fatalf("joined ledger failure = %v", err)
	}
	readErr := errors.New("read failed")
	store = &fakeStore{settings: model.AgentRetentionSettings{Configured: true, RolloutDays: 1}, getErr: readErr, failGetFrom: 2}
	service = New(store, &fakePruner{}, agentrollout.RetentionPolicy{})
	if _, err := service.Run(t.Context()); !errors.Is(err, readErr) {
		t.Fatalf("joined read failure = %v", err)
	}
	store = &fakeStore{getErr: readErr}
	if _, err := New(store, &fakePruner{}, agentrollout.RetentionPolicy{}).Run(t.Context()); !errors.Is(err, readErr) {
		t.Fatalf("Run hid an effective-policy failure: %v", err)
	}
}

func TestServiceRunScheduledPrunesOnlyWhenEnabled(t *testing.T) {
	store, pruner := &fakeStore{}, &fakePruner{}
	if err := New(store, pruner, agentrollout.RetentionPolicy{}).RunScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pruner.policies) != 0 || len(store.marks) != 0 {
		t.Fatal("disabled schedule touched the pruner or the run ledger")
	}
	store = &fakeStore{settings: model.AgentRetentionSettings{Configured: true, ReleaseDays: 14}}
	pruner = &fakePruner{}
	if err := New(store, pruner, agentrollout.RetentionPolicy{}).RunScheduled(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pruner.policies) != 1 || len(store.marks) != 1 || store.marks[0].trigger != model.AgentRetentionTriggerSchedule {
		t.Fatalf("scheduled prune = %+v, marks = %+v", pruner.policies, store.marks)
	}
	store = &fakeStore{getErr: errors.New("read failed")}
	if err := New(store, &fakePruner{}, agentrollout.RetentionPolicy{}).RunScheduled(t.Context()); err == nil {
		t.Fatal("RunScheduled ignored a read failure")
	}
}
