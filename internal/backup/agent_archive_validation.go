package backup

import (
	"time"

	"github.com/beat/backend/internal/model"
)

func archiveMetricCutoff(manifest model.BackupManifest) (time.Time, error) {
	if manifest.FormatVersion >= 3 {
		return model.AgentArchiveEnd(manifest.SnapshotCutoff)
	}
	return manifest.SnapshotCutoff, nil
}
