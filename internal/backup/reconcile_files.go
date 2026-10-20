package backup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const (
	backupPartialSuffix          = ".partial"
	backupArchivePrefix          = "beat-"
	backupArchiveSuffix          = ".zip"
	backupQuarantineDir          = "quarantine"
	backupJournalPrefix          = ".restore-journal-"
	backupQuarantineReasonOrphan = "orphan"
	quarantineSuffixBytes        = 8
)

// sweepFilesystem 只清理可确认由 Beat 生成的路径：*.partial 直接删除，
// 孤儿归档移入私有隔离清单，其他文件、目录与符号链接一律不动。
func (reconciler *reconciler) sweepFilesystem(records []model.BackupRecord) error {
	known := map[string]bool{}
	for _, record := range records {
		known[record.Filename] = true
	}
	entries, err := os.ReadDir(reconciler.service.root)
	if err != nil {
		return fmt.Errorf("read backup directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == backupQuarantineDir || known[entry.Name()] {
			continue
		}
		if err := reconciler.sweepEntry(entry.Name()); err != nil {
			return err
		}
	}
	return reconciler.sweepStaleJournals()
}

func (reconciler *reconciler) sweepEntry(name string) error {
	path := filepath.Join(reconciler.service.root, name)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect backup directory entry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if strings.HasSuffix(name, backupPartialSuffix) {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale backup partial: %w", err)
		}
		reconciler.outcome.PartialRemoved++
		return nil
	}
	if !strings.HasPrefix(name, backupArchivePrefix) || !strings.HasSuffix(name, backupArchiveSuffix) {
		return nil
	}
	return reconciler.quarantine(name, info.Size())
}

// quarantine 先把孤儿归档移出活动目录，再登记清单；登记失败只降级为延后，
// 因为归档已经位于私有隔离目录，不会再被误用。
func (reconciler *reconciler) quarantine(name string, size int64) error {
	directory := filepath.Join(reconciler.service.root, backupQuarantineDir)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create backup quarantine directory: %w", err)
	}
	destination := filepath.Join(directory, quarantineName(directory, name))
	if err := os.Rename(filepath.Join(reconciler.service.root, name), destination); err != nil {
		return fmt.Errorf("quarantine orphan backup archive: %w", err)
	}
	record := model.BackupQuarantineRecord{ID: uuid.NewString(), Filename: name, SizeBytes: size,
		Reason: backupQuarantineReasonOrphan, QuarantinedAt: reconciler.service.now()}
	if err := reconciler.service.records.RecordQuarantine(reconciler.ctx, &record); err != nil {
		slog.WarnContext(reconciler.ctx, "quarantine inventory write failed", "error", err)
		reconciler.outcome.Deferred++
		return nil
	}
	reconciler.outcome.Quarantined++
	return nil
}

// quarantineName 避免覆盖既有隔离归档，重名时追加随机后缀。
func quarantineName(directory, name string) string {
	if _, err := os.Lstat(filepath.Join(directory, name)); os.IsNotExist(err) {
		return name
	}
	suffix := uuid.NewString()
	return strings.TrimSuffix(name, backupArchiveSuffix) + "-" +
		suffix[:quarantineSuffixBytes] + backupArchiveSuffix
}

// sweepStaleJournals 删除 rename 前中断留下的 journal 临时文件。
func (reconciler *reconciler) sweepStaleJournals() error {
	dataDir := filepath.Dir(reconciler.service.root)
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), backupJournalPrefix) {
			continue
		}
		path := filepath.Join(dataDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale restore journal: %w", err)
		}
		reconciler.outcome.PartialRemoved++
	}
	return nil
}
