package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (service *Service) ValidateUpload(
	ctx context.Context,
	source io.Reader,
) (model.BackupRecord, error) {
	id := uuid.New().String()
	filename := fmt.Sprintf("beat-upload-v1-%s.zip", id)
	path, err := service.archivePath(filename)
	if err != nil {
		return model.BackupRecord{}, err
	}
	partial := path + ".partial"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return model.BackupRecord{}, fmt.Errorf("create uploaded backup: %w", err)
	}
	size, copyErr := copyBounded(file, source, MaximumUploadBytes)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		_ = os.Remove(partial)
		return model.BackupRecord{}, err
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return model.BackupRecord{}, fmt.Errorf("publish uploaded backup: %w", err)
	}
	validated, err := validateArchive(ctx, path, service.root)
	if err != nil {
		_ = os.Remove(path)
		return model.BackupRecord{}, err
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	completed := service.now()
	record := model.BackupRecord{ID: id, Filename: filename, Source: "uploaded",
		State: model.BackupStateValidated, CreatedAt: completed, UpdatedAt: completed,
		CompletedAt:    &completed,
		SnapshotCutoff: &validated.manifest.SnapshotCutoff, SizeBytes: size,
		SQLiteBytes:  validated.manifest.PayloadSizes[sqliteEntry],
		MetricsBytes: validated.manifest.PayloadSizes[metricsEntry],
		MetricRows:   validated.manifest.MetricRows}
	if err := service.records.Create(ctx, &record); err != nil {
		_ = os.Remove(path)
		return model.BackupRecord{}, err
	}
	return record, nil
}

// StageRequest 汇总一次恢复暂存请求：备份 ID、确认短语与审计主体。
type StageRequest struct {
	BackupID     string
	Confirmation string
	Actor        Actor
}

// StageRestore 先提交 staging 与随机代次，再写私有 journal，最后提交 staged；
// 任一步失败都会回滚，绝不留下“journal 已写但没有 staged 授权”的可用恢复。
func (service *Service) StageRestore(
	ctx context.Context,
	request StageRequest,
) (model.BackupRecord, error) {
	if request.Confirmation != RestoreConfirmation {
		return model.BackupRecord{}, ErrInvalidConfirm
	}
	release, err := service.acquireSecretLifecycle(ctx)
	if err != nil {
		return model.BackupRecord{}, err
	}
	defer release()
	record, err := service.record(ctx, request.BackupID)
	if err != nil {
		return model.BackupRecord{}, err
	}
	if record.State != model.BackupStateReady && record.State != model.BackupStateValidated {
		return model.BackupRecord{}, errors.New("backup archive cannot be staged")
	}
	// 已存在别的待应用 journal 时必须拒绝，避免覆盖另一个暂存恢复。
	if err := service.rejectConflictingJournal(request.BackupID); err != nil {
		return model.BackupRecord{}, err
	}
	path, err := service.archivePath(record.Filename)
	if err != nil {
		return model.BackupRecord{}, err
	}
	validated, err := validateArchive(ctx, path, service.root)
	if err != nil {
		return model.BackupRecord{}, err
	}
	if err := os.RemoveAll(validated.root); err != nil {
		return model.BackupRecord{}, fmt.Errorf("remove restore validation directory: %w", err)
	}
	generation := uuid.NewString()
	staging := *record
	staging.State = model.BackupStateStaging
	staging.PreviousState = record.State
	staging.RestoreGeneration = generation
	staging.UpdatedAt = service.now()
	if err := service.transition(ctx, transitionRequest{record: staging,
		expected: []string{record.State}, actor: request.Actor,
		action: "backup.restore.staging"}); err != nil {
		return model.BackupRecord{}, err
	}
	journal := model.RestoreJournal{BackupID: record.ID, ArchivePath: path, State: "pending",
		Generation: generation, CreatedAt: service.now()}
	if err := writeJournal(service.journalPath(), journal); err != nil {
		return model.BackupRecord{}, errors.Join(err,
			service.abortStaging(ctx, record.ID, request.Actor))
	}
	staged := staging
	staged.State = model.BackupStateStaged
	staged.UpdatedAt = service.now()
	if err := service.transition(ctx, transitionRequest{record: staged,
		expected: []string{model.BackupStateStaging}, actor: request.Actor,
		action: "backup.restore.staged"}); err != nil {
		return model.BackupRecord{}, errors.Join(err,
			service.abortStaging(ctx, record.ID, request.Actor))
	}
	return staged, nil
}

func writeJournal(path string, journal model.RestoreJournal) error {
	content, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode restore journal: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".restore-journal-")
	if err != nil {
		return fmt.Errorf("create restore journal: %w", err)
	}
	temporary := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return fmt.Errorf("secure restore journal: %w", err)
	}
	_, writeErr := file.Write(append(content, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write restore journal: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish restore journal: %w", err)
	}
	return nil
}

func readJournal(path string) (model.RestoreJournal, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.RestoreJournal{}, err
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	var journal model.RestoreJournal
	if err := decoder.Decode(&journal); err != nil {
		return model.RestoreJournal{}, fmt.Errorf("decode restore journal: %w", err)
	}
	if journal.BackupID == "" || journal.ArchivePath == "" || journal.State == "" || journal.CreatedAt.IsZero() {
		return model.RestoreJournal{}, errors.New("restore journal is invalid")
	}
	return journal, nil
}

func journalTimestamp(now time.Time) string {
	return now.UTC().Format("20060102T150405.000000000Z")
}
