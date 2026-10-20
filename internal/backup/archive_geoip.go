package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/geoip"
	"github.com/beat/backend/internal/model"
)

const geoIPCurrentEntry = "geoip/country.mmdb"

const geoIPPreviousEntry = "geoip/country.mmdb.previous"

// GeoIPAssets 在发布锁内捕获数据库快照和固定资产，避免新旧版本交叉。
type GeoIPAssets interface {
	WithBackupAssets(context.Context, func([]geoip.BackupAsset) error) error
}

// SetGeoIPAssets 仅在启动阶段配置；外部只读挂载仅记录，不复制。
func (service *Service) SetGeoIPAssets(assets GeoIPAssets, externalPath string) {
	service.geoipAssets, service.externalMMDB = assets, externalPath
}

func validGeoIPEntry(name string) bool {
	return name == geoIPCurrentEntry || name == geoIPPreviousEntry
}

func captureGeoIPAssets(root string, assets []geoip.BackupAsset, captured *archiveCapture) error {
	captured.geoip = map[string]model.MMDBMetadata{}
	for _, asset := range assets {
		name := "geoip/" + asset.Name
		if !validGeoIPEntry(name) {
			return errors.New("unexpected GeoIP backup asset")
		}
		if _, duplicate := captured.geoip[name]; duplicate {
			return errors.New("duplicate GeoIP backup asset")
		}
		metadata, err := copyGeoIPAsset(root, asset)
		if err != nil {
			return err
		}
		captured.geoip[name] = metadata
		captured.payloads = append(captured.payloads, name)
	}
	return nil
}

func copyGeoIPAsset(root string, asset geoip.BackupAsset) (model.MMDBMetadata, error) {
	var empty model.MMDBMetadata
	info, err := os.Lstat(asset.Path)
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > geoip.MaximumMMDBBytes {
		return empty, errors.New("unsafe GeoIP backup asset")
	}
	directory := filepath.Join(root, "geoip")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return empty, err
	}
	path := filepath.Join(directory, asset.Name)
	if err := copyFile(asset.Path, path, 0o600); err != nil {
		return empty, err
	}
	metadata, err := geoip.ValidateAsset(path)
	if err != nil {
		return empty, err
	}
	if !sameGeoIPMetadata(metadata, asset.Metadata) {
		return empty, errors.New("GeoIP asset changed during capture")
	}
	metadata.Source = "managed"
	return metadata, nil
}

func sameGeoIPMetadata(left, right model.MMDBMetadata) bool {
	return left.SHA256 == right.SHA256 && left.SizeBytes == right.SizeBytes && left.BuildEpoch == right.BuildEpoch && left.Edition == right.Edition
}

func validateGeoIPPayloads(root string, manifest model.BackupManifest) error {
	if err := validateGeoIPManifest(manifest); err != nil {
		return err
	}
	for _, name := range []string{geoIPCurrentEntry, geoIPPreviousEntry} {
		if err := validateGeoIPPayload(root, name, manifest); err != nil {
			return err
		}
	}
	return validateGeoIPSnapshot(root, manifest)
}

func validateGeoIPManifest(manifest model.BackupManifest) error {
	if manifest.FormatVersion < 4 && (len(manifest.GeoIPFiles) != 0 || manifest.ExternalMMDB != "") {
		return errors.New("GeoIP payload requires backup format 4")
	}
	if manifest.ExternalMMDB != "" && (!filepath.IsAbs(manifest.ExternalMMDB) || len(manifest.GeoIPFiles) != 0) {
		return errors.New("external MMDB metadata is inconsistent")
	}
	for name := range manifest.GeoIPFiles {
		if !validGeoIPEntry(name) {
			return errors.New("unexpected GeoIP manifest asset")
		}
	}
	if _, previous := manifest.GeoIPFiles[geoIPPreviousEntry]; previous {
		if _, current := manifest.GeoIPFiles[geoIPCurrentEntry]; !current {
			return errors.New("previous MMDB requires a current database")
		}
	}
	return nil
}

func validateGeoIPPayload(root, name string, manifest model.BackupManifest) error {
	declared, exists := manifest.GeoIPFiles[name]
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	_, checksummed := manifest.Checksums[name]
	if exists != (err == nil) || exists != checksummed {
		return fmt.Errorf("GeoIP asset %s metadata is inconsistent", name)
	}
	if !exists {
		return nil
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("unsafe archived MMDB")
	}
	actual, err := geoip.ValidateAsset(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	if !sameGeoIPMetadata(actual, declared) || manifest.PayloadSizes[name] != actual.SizeBytes || manifest.Checksums[name] != actual.SHA256 {
		return errors.New("archived MMDB metadata mismatch")
	}
	return nil
}
