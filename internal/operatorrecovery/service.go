package operatorrecovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type Config struct{ DataDir, DatabasePath string }

type Session struct {
	config Config
	lease  *applicationsecrets.Lease
	Status Status
}

// Open 获取正常启动使用的锁，所有检查仅作用于私有副本。
func Open(ctx context.Context, config Config) (session *Session, resultErr error) {
	config, err := resolveConfig(config)
	if err != nil {
		return nil, err
	}
	lease, err := applicationsecrets.AcquireLease(config.DataDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, lease.Close())
		}
	}()
	if err := validateDirectory(config.DataDir); err != nil {
		return nil, err
	}
	status, err := inspectInstance(ctx, config)
	if err != nil {
		return nil, err
	}
	return &Session{config: config, lease: lease, Status: status}, nil
}

func resolveConfig(config Config) (Config, error) {
	if config.DataDir == "" {
		return config, errors.New("data directory is required")
	}
	directory, err := filepath.Abs(config.DataDir)
	if err != nil {
		return config, err
	}
	config.DataDir = directory
	if config.DatabasePath == "" {
		config.DatabasePath = filepath.Join(directory, "beat.db")
	}
	config.DatabasePath, err = filepath.Abs(config.DatabasePath)
	if err != nil {
		return config, err
	}
	if filepath.Dir(config.DatabasePath) != directory {
		return config, errors.New("database must be directly inside the data directory")
	}
	if err := privatePath(directory, true); err != nil {
		return config, err
	}
	return config, nil
}

func inspectInstance(ctx context.Context, config Config) (Status, error) {
	pending, err := backup.PendingRestore(config.DataDir)
	if err != nil {
		return Status{}, err
	}
	if _, err := os.Lstat(config.DatabasePath); errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(config.DataDir)
		if readErr != nil {
			return Status{}, readErr
		}
		if pending && stagedEntries(entries) {
			return Status{State: "restore_staged", PendingRestore: true, Owners: []Owner{}, NextActions: []string{"start-server"}}, nil
		}
		if len(entries) != 0 {
			return Status{}, errors.New("database absent in a nonempty directory; resolve pending restore or unknown files first")
		}
		return Status{State: "empty_instance", Owners: []Owner{}, NextActions: []string{"stage-restore", "start-server"}}, nil
	} else if err != nil {
		return Status{}, err
	}
	status, err := inspectDatabase(ctx, config)
	status.PendingRestore = pending
	if pending {
		status.NextActions = []string{"start-server"}
	}
	return status, err
}

func (session *Session) Close() error { return session.lease.Close() }

func (session *Session) SelectOwner(username string) (model.AdminUser, error) {
	normalized := model.NormalizeUsername(username)
	var matches []model.AdminUser
	for _, user := range session.Status.users {
		if model.NormalizeUsername(user.Username) == normalized {
			matches = append(matches, user)
		}
	}
	if len(matches) != 1 || matches[0].Role != model.AdminRoleOwner {
		return model.AdminUser{}, errors.New("select one exact existing owner username; use validated backup restore if no owner exists")
	}
	return matches[0], nil
}

func (session *Session) Confirmation(username string) string {
	return fmt.Sprintf("UNLOCK %q / %s", session.Status.Site, username)
}

type UnlockInput struct{ Username, Password, Repeat, Confirmation string }

func (session *Session) Unlock(ctx context.Context, input UnlockInput) error {
	if session.Status.PendingRestore {
		return errors.New("pending restore must be resolved before recovery")
	}
	owner, err := session.SelectOwner(input.Username)
	if err != nil {
		return err
	}
	if input.Confirmation != session.Confirmation(owner.Username) {
		return errors.New("owner recovery confirmation does not match")
	}
	if input.Password != input.Repeat {
		return errors.New("replacement passwords do not match")
	}
	if err := model.ValidateAdminPassword(input.Password); err != nil {
		return err
	}
	hash, err := adminauth.NewPasswordHasher(adminauth.DefaultPasswordParams(), nil).Hash(input.Password)
	if err != nil {
		return errors.New("replacement password hashing failed")
	}
	if err := validateDirectory(session.config.DataDir); err != nil {
		return err
	}
	database, err := openDatabase(session.config.DatabasePath, true)
	if err != nil {
		return err
	}
	write := store.AdminRecoveryWrite{UserID: owner.ID, Username: owner.Username, PasswordHash: hash, SecretGeneration: session.Status.generation, RootGeneration: session.Status.rootGeneration, At: time.Now().UTC()}
	return errors.Join(store.NewAdminStore(database).RecoverOwner(ctx, write), database.Close())
}

func (session *Session) StageRestore(ctx context.Context, archive string) (resultErr error) {
	if session.Status.PendingRestore {
		return errors.New("pending restore already exists")
	}
	file, err := openPrivate(archive)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := validateDirectory(session.config.DataDir); err != nil {
		return err
	}
	return backup.StageOffline(ctx, session.config.DataDir, io.Reader(file))
}

// CreateEmptyDirectory 仅创建精确目录，不创建父路径或初始化数据库。
func CreateEmptyDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return privatePath(path, true)
}

func stagedEntries(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() != "backups" && entry.Name() != "restore.pending.json" {
			return false
		}
	}
	return true
}
