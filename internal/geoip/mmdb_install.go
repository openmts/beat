package geoip

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/model"
)

func privateDirectory(path string) error {
	if path == "" {
		return ErrUnavailable
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return ErrUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) stageDatabase(reader io.Reader) (string, error) {
	if err := privateDirectory(s.directory); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(s.directory, ".mmdb-upload-")
	if err != nil {
		return "", ErrUnavailable
	}
	name := file.Name()
	size, copyErr := io.Copy(file, io.LimitReader(reader, MaximumMMDBBytes+1))
	err = errors.Join(copyErr, file.Sync(), file.Close())
	if err != nil || size > MaximumMMDBBytes {
		_ = os.Remove(name)
		return "", ErrInvalid
	}
	return name, nil
}

func (s *Service) InstallMMDB(ctx context.Context, actor Actor, input MMDBUpload) (model.GeoIPSettings, error) {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	s.assetMu.Lock()
	defer s.assetMu.Unlock()
	if s.external != "" {
		return model.GeoIPSettings{}, ErrForbidden
	}
	// 在读取大请求体之前先检查会话；提交前再次复验。
	err := s.write(ctx, func(tx transaction) error { return tx.authorize(actor, true, true) })
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	path, err := s.stageDatabase(input.Body)
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	defer func() { _ = os.Remove(path) }()
	candidate, metadata, err := validateDatabase(path)
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	defer func() {
		if candidate != nil {
			_ = candidate.Close()
		}
	}()
	metadata.Source = input.source
	if metadata.Source == "" {
		metadata.Source = "upload"
	}
	if err = s.commitDatabase(ctx, actor, databaseCommit{path: path, metadata: metadata, revision: input.ExpectedRevision}); err != nil {
		return model.GeoIPSettings{}, err
	}
	if err = s.mmdb.replace(candidate); err != nil {
		candidate = nil
		return model.GeoIPSettings{}, ErrUnavailable
	}
	candidate = nil
	if err = s.finishPublication(); err != nil {
		return model.GeoIPSettings{}, err
	}
	return s.Settings(ctx, actor)
}

type MMDBUpload struct {
	Body             io.Reader
	ExpectedRevision int64
	source           string
}

func (tx transaction) recordDatabase(actor Actor, metadata model.MMDBMetadata) error {
	_, err := tx.db.ExecContext(tx.ctx, `UPDATE geoip_settings SET provider=CASE WHEN provider='disabled' THEN 'mmdb' ELSE provider END,
 revision=revision+1,mmdb_source=?,mmdb_edition=?,mmdb_build_epoch=?,mmdb_sha256=?,mmdb_size=?,mmdb_state='ready',mmdb_error='',updated_at=? WHERE id='global'`, metadata.Source, metadata.Edition, metadata.BuildEpoch, metadata.SHA256, metadata.SizeBytes, tx.now)
	if err != nil {
		return ErrUnavailable
	}
	if _, err = tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET next_refresh_at=?,lease_hash='',lease_expires_at=NULL WHERE mode IN('observed','managed_target') AND source_encrypted IS NOT NULL`, tx.now); err != nil {
		return ErrUnavailable
	}
	return tx.audit(actor, "geoip.mmdb.install", "global")
}

func (s *Service) LoadDatabase() error {
	s.assetMu.Lock()
	defer s.assetMu.Unlock()
	if err := s.RecoverPublication(); err != nil {
		return err
	}
	path := s.external
	if path == "" {
		path = filepath.Join(s.directory, "country.mmdb")
	}
	reader, _, err := validateDatabase(path)
	if err != nil {
		return err
	}
	return s.mmdb.replace(reader)
}

type databaseCommit struct {
	path     string
	metadata model.MMDBMetadata
	revision int64
}

func (s *Service) commitDatabase(ctx context.Context, actor Actor, input databaseCommit) error {
	var rollback func() error
	err := s.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		current, err := scanSettings(tx.db.QueryRowContext(ctx, settingsSelect))
		if err != nil {
			return ErrUnavailable
		}
		if current.Revision != input.revision {
			return ErrConflict
		}
		rollback, err = s.publishDatabase(input.path)
		if err != nil {
			return err
		}
		return tx.recordDatabase(actor, input.metadata)
	})
	if err != nil && rollback != nil {
		return errors.Join(err, rollback())
	}
	return err
}
