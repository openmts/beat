package maintenance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeSecurity struct {
	cutoff time.Time
	batch  int
	err    error
}

func (security *fakeSecurity) PurgeSecurityData(_ context.Context, cutoff time.Time, batch int) (int64, error) {
	security.cutoff, security.batch = cutoff, batch
	return 1, security.err
}

func TestServicePurgesSecurityDataWithRegisteredStore(t *testing.T) {
	settings := &fakeSettingsStore{settings: model.DefaultMaintenanceSettings()}
	security := &fakeSecurity{}
	service, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{}, WithSecurityStore(security))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	now := time.Date(2026, 7, 30, 5, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if err := service.StartManual(); err != nil {
		t.Fatalf("start maintenance: %v", err)
	}
	service.Wait()
	if security.batch != securityRetentionBatch {
		t.Fatalf("security purge batch = %d, want %d", security.batch, securityRetentionBatch)
	}
	if !security.cutoff.Equal(now) {
		t.Fatalf("security purge cutoff = %v, want %v", security.cutoff, now)
	}
	_, status, err := settings.Get(t.Context())
	if err != nil || status.LastStatus != model.MaintenanceStatusSuccess {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}

func TestServiceReportsSecurityPurgeFailure(t *testing.T) {
	settings := &fakeSettingsStore{settings: model.DefaultMaintenanceSettings()}
	security := &fakeSecurity{err: errors.New("security purge failed")}
	service, err := NewService(t.Context(), settings, &fakeSQLite{}, &fakeMTS{}, WithSecurityStore(security))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if err := service.StartManual(); err != nil {
		t.Fatalf("start maintenance: %v", err)
	}
	service.Wait()
	_, status, err := settings.Get(t.Context())
	if err != nil || status.LastStatus != model.MaintenanceStatusFailed ||
		!strings.Contains(status.LastError, "security purge failed") {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}
