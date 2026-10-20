package geoip

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/model"
)

type BackupAsset struct {
	Name     string             `json:"name"`
	Path     string             `json:"-"`
	Metadata model.MMDBMetadata `json:"metadata"`
}

// ValidateAsset 校验备份内MMDB；调用方负责归档路径与权限检查。
func ValidateAsset(path string) (model.MMDBMetadata, error) {
	reader, metadata, err := validateDatabase(path)
	if err != nil {
		return metadata, err
	}
	return metadata, reader.Close()
}

// BackupAssets 外挂只读资产不复制；仅返回托管的current/previous固定清单。
func (s *Service) BackupAssets(ctx context.Context) ([]BackupAsset, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	assets := []BackupAsset{}
	if s.external != "" {
		return assets, nil
	}
	for _, name := range []string{"country.mmdb", "country.mmdb.previous"} {
		asset, err := s.backupAsset(name)
		if err != nil {
			return nil, err
		}
		if asset != nil {
			assets = append(assets, *asset)
		}
	}
	return assets, nil
}

func (s *Service) backupAsset(name string) (*BackupAsset, error) {
	path := filepath.Join(s.directory, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrUnavailable
	}
	metadata, err := ValidateAsset(path)
	if err != nil {
		return nil, err
	}
	metadata.Source = "managed"
	return &BackupAsset{Name: name, Path: path, Metadata: metadata}, nil
}

// WithBackupAssets 保持发布互斥直到调用方完成文件复制与清单捕获。
func (s *Service) WithBackupAssets(ctx context.Context, capture func([]BackupAsset) error) error {
	s.assetMu.Lock()
	defer s.assetMu.Unlock()
	assets, err := s.BackupAssets(ctx)
	if err != nil {
		return err
	}
	return capture(assets)
}
