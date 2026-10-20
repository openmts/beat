package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// StageOffline 供已持有生命周期锁并校验数据目录的本机命令使用；不需要现存数据库。
func StageOffline(ctx context.Context, dataDir string, source io.Reader) (resultErr error) {
	if pending, err := PendingRestore(dataDir); err != nil || pending {
		return errors.Join(err, errors.New("pending restore must be resolved before staging"))
	}
	temporary, err := os.MkdirTemp(filepath.Dir(dataDir), ".beat-offline-restore-")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(temporary)) }()
	archive := filepath.Join(temporary, "archive.zip")
	if err := writeOfflineArchive(archive, source); err != nil {
		return err
	}
	validated, err := validateArchive(ctx, archive, temporary)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(validated.root); err != nil {
		return err
	}
	return publishOfflineArchive(dataDir, archive)
}

func writeOfflineArchive(path string, source io.Reader) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := copyBounded(file, source, MaximumUploadBytes)
	return errors.Join(copyErr, file.Sync(), file.Close())
}

func publishOfflineArchive(dataDir, archive string) error {
	root := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	id := uuid.NewString()
	destination := filepath.Join(root, "beat-offline-"+id+".zip")
	if err := os.Rename(archive, destination); err != nil {
		return err
	}
	journal := model.RestoreJournal{BackupID: id, ArchivePath: destination, State: "pending",
		Origin: model.RestoreOriginOffline, CreatedAt: time.Now().UTC()}
	if err := writeJournal(filepath.Join(dataDir, "restore.pending.json"), journal); err != nil {
		return errors.Join(err, os.Remove(destination))
	}
	return nil
}
