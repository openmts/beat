package backup

import (
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestServiceQuarantineListingsWithoutUploadStore(t *testing.T) {
	empty := &Service{}
	uploads, err := empty.ListQuarantinedUploads(t.Context())
	if err != nil || len(uploads) != 0 {
		t.Fatalf("quarantined uploads = %v/%v", uploads, err)
	}
	fixture := newBackupFixture(t)
	records, err := fixture.service.ListQuarantine(t.Context())
	if err != nil || len(records) != 0 {
		t.Fatalf("quarantine records = %v/%v", records, err)
	}
}

func TestRestoreAuthorizationIgnoresUnknownBackup(t *testing.T) {
	fixture := newBackupFixture(t)
	journal := model.RestoreJournal{BackupID: "missing-backup", Generation: "generation", State: model.BackupStateStaged}
	authorized, err := restoreAuthorized(t.Context(), filepath.Join(fixture.dataDir, "beat.db"), journal)
	if err != nil || authorized {
		t.Fatalf("restore authorization = %v/%v", authorized, err)
	}
}

func TestValidateArchivedMetricErasureJobRejectsFleetTarget(t *testing.T) {
	job := validArchivedErasureJob()
	job.Scope = model.MetricErasureFleetTelemetry
	job.TargetType = model.MetricErasureTargetTypeFor(job.Scope)
	if job.TargetType != "" {
		t.Fatalf("fleet erasure target type = %q", job.TargetType)
	}
	job.TargetID = "unexpected-target"
	if err := validateArchivedMetricErasureJob(job, nil, nil); err == nil {
		t.Fatal("fleet erasure job carrying a target accepted")
	}
}
