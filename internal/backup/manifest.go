package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

const maximumManifestBytes = 1 << 20

func readManifest(path string) (model.BackupManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.BackupManifest{}, fmt.Errorf("open backup manifest: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() > maximumManifestBytes {
		return model.BackupManifest{}, errors.New("backup manifest exceeds its limit")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumManifestBytes+1))
	if err != nil || len(content) > maximumManifestBytes {
		return model.BackupManifest{}, errors.New("backup manifest exceeds its limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest model.BackupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode backup manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return manifest, errors.New("backup manifest contains trailing data")
	}
	if err := validateManifestHeader(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validateManifestHeader(manifest model.BackupManifest) error {
	if manifest.CreatedAt.IsZero() || manifest.SnapshotCutoff.IsZero() || manifest.MetricRows < 0 || manifest.MetricRows > maximumMetricRows {
		return errors.New("backup manifest is incompatible")
	}
	if err := validateAgentReleaseManifest(manifest.AgentReleaseCache); err != nil {
		return err
	}
	return validateManifestVersion(manifest)
}

func validateManifestVersion(manifest model.BackupManifest) error {
	if err := validateOptionalManifestVersion(manifest); err != nil {
		return err
	}
	switch manifest.FormatVersion {
	case 1:
		return nil
	case 2:
		if len(manifest.ThemeFiles) == 0 {
			return errors.New("backup format 2 requires theme metadata")
		}
	case 3, 4:
		if manifest.CatalogVersion != metriccatalog.Version || manifest.CatalogRevision < 1 {
			return errors.New("backup format requires a compatible metric catalog")
		}
	default:
		return errors.New("backup manifest is incompatible")
	}
	return nil
}

func validateOptionalManifestVersion(manifest model.BackupManifest) error {
	if manifest.FormatVersion < 4 && manifest.AgentReleaseCache != nil {
		return errors.New("agent release metadata requires backup format 4")
	}
	if manifest.FormatVersion < 4 && (len(manifest.GeoIPFiles) != 0 || manifest.ExternalMMDB != "") {
		return errors.New("GeoIP metadata requires backup format 4")
	}
	return nil
}
