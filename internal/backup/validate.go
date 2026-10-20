package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

const (
	maximumArchiveEntries = 600
	maximumMetricRows     = model.MaximumBackupMetricRows
	maximumJSONLLine      = 1 << 20
)

type validatedArchive struct {
	manifest model.BackupManifest
	root     string
}

func validateArchive(ctx context.Context, archivePath, stagingParent string) (*validatedArchive, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("inspect backup archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaximumUploadBytes {
		return nil, errors.New("backup archive size or type is invalid")
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open backup archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	root, err := os.MkdirTemp(stagingParent, ".restore-validate-")
	if err != nil {
		return nil, fmt.Errorf("create restore validation directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("secure restore validation directory: %w", err)
	}
	if err := extractAllowlisted(reader.File, root); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	manifest, err := validateExtracted(ctx, root)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	return &validatedArchive{manifest: manifest, root: root}, nil
}

func extractAllowlisted(files []*zip.File, root string) error {
	if len(files) < 4 || len(files) > maximumArchiveEntries {
		return errors.New("backup archive entry count is invalid")
	}
	seen := map[string]bool{}
	var expanded uint64
	for _, file := range files {
		if err := validateArchiveEntry(file, seen); err != nil {
			return err
		}
		seen[file.Name] = true
		if file.UncompressedSize64 > uint64(MaximumExpandedBytes)-expanded {
			return errors.New("backup archive expanded size exceeds limit")
		}
		expanded += file.UncompressedSize64
		if err := extractEntry(file, root); err != nil {
			return err
		}
	}
	for _, required := range []string{manifestEntry, sqliteEntry, metricsEntry, checksumsEntry} {
		if !seen[required] {
			return fmt.Errorf("backup archive is missing %s", required)
		}
	}
	return nil
}

func extractEntry(file *zip.File, root string) error {
	destination := filepath.Join(root, filepath.FromSlash(file.Name))
	if filepath.Dir(destination) != root {
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create restore entry directory: %w", err)
		}
	}
	input, err := file.Open()
	if err != nil {
		return fmt.Errorf("open restore entry %s: %w", file.Name, err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = input.Close()
		return fmt.Errorf("create restore entry %s: %w", file.Name, err)
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, int64(file.UncompressedSize64)+1))
	syncErr := output.Sync()
	closeOutputErr := output.Close()
	closeInputErr := input.Close()
	if err := errors.Join(copyErr, syncErr, closeOutputErr, closeInputErr); err != nil {
		return fmt.Errorf("extract restore entry %s: %w", file.Name, err)
	}
	if written != int64(file.UncompressedSize64) {
		return fmt.Errorf("restore entry %s size is invalid", file.Name)
	}
	return nil
}

func validateExtracted(ctx context.Context, root string) (model.BackupManifest, error) {
	manifest, err := readManifest(filepath.Join(root, manifestEntry))
	if err != nil {
		return model.BackupManifest{}, err
	}
	if err := validateExtractedPayloads(ctx, root, manifest); err != nil {
		return model.BackupManifest{}, err
	}
	inventory, err := archivedMetricInventory(ctx, root, manifest)
	if err != nil {
		return model.BackupManifest{}, err
	}
	metricCutoff, err := archiveMetricCutoff(manifest)
	if err != nil {
		return model.BackupManifest{}, err
	}
	rows, err := validateCatalogMetricsContext(ctx, metricValidationInput{path: filepath.Join(root, metricsEntry), cutoff: metricCutoff.UnixNano(), definitions: inventory})
	if err != nil {
		return model.BackupManifest{}, err
	}
	if rows != manifest.MetricRows {
		return model.BackupManifest{}, errors.New("MTS row count does not match manifest")
	}
	if checksumErr := validateChecksumList(root, manifest); checksumErr != nil {
		return model.BackupManifest{}, checksumErr
	}
	return manifest, nil
}

func validThemeBackupPath(name string) bool {
	// 要求 themes/<64位小写hex>/<相对安全路径>。
	parts := strings.Split(name, "/")
	if len(parts) < 3 || parts[0] != "themes" || !isHexString(parts[1], 64) {
		return false
	}
	for _, part := range parts[2:] {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validatePayloads(root string, manifest model.BackupManifest) error {
	required := map[string]bool{sqliteEntry: true, metricsEntry: true}
	for name := range manifest.Checksums {
		if name != sqliteEntry && name != metricsEntry && name != dataKeyEntry &&
			!strings.HasPrefix(name, "themes/") && !validGeoIPEntry(name) {
			return fmt.Errorf("manifest payload %q is not allowed", name)
		}
		required[name] = true
	}
	if len(manifest.Checksums) != len(manifest.PayloadSizes) {
		return errors.New("manifest payload metadata is inconsistent")
	}
	_, keyErr := os.Stat(filepath.Join(root, filepath.FromSlash(dataKeyEntry)))
	_, keyDeclared := manifest.Checksums[dataKeyEntry]
	if (keyErr == nil) != keyDeclared || (keyErr != nil && !os.IsNotExist(keyErr)) {
		return errors.New("administrator data key metadata is inconsistent")
	}
	for name := range required {
		size, checksum, err := fileDigest(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if manifest.PayloadSizes[name] != size || manifest.Checksums[name] != checksum {
			return fmt.Errorf("backup payload %s failed checksum validation", name)
		}
		if strings.HasPrefix(name, "themes/") {
			themeFile, ok := manifest.ThemeFiles[name]
			if !ok || themeFile.Size != size || themeFile.SHA256 != checksum {
				return fmt.Errorf("backup theme payload %s metadata is inconsistent", name)
			}
		}
	}
	if _, ok := manifest.Checksums[dataKeyEntry]; ok {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dataKeyEntry)))
		if err != nil || info.Size() != 32 {
			return errors.New("administrator data key is invalid")
		}
	}
	if manifest.FormatVersion >= 2 {
		if err := validateThemePayloads(root, manifest); err != nil {
			return err
		}
	}
	return nil
}

// validateThemePayloads 校验归档内主题包结构一致且活动主题资产完整。
func validateThemePayloads(root string, manifest model.BackupManifest) error {
	packages := map[string]bool{}
	for name := range manifest.ThemeFiles {
		parts := strings.Split(name, "/")
		if len(parts) >= 2 {
			packages[parts[1]] = true
		}
	}
	for digest := range packages {
		archivePath := filepath.Join(root, filepath.FromSlash("themes/"+digest+"/archive.zip"))
		if _, err := os.Stat(archivePath); err != nil {
			return fmt.Errorf("theme package %s is missing its archive", digest)
		}
	}
	// 活动主题包必须存在（通过归档 SQLite 的活动主题记录）。
	activeDigest, err := archivedActiveThemeDigest(filepath.Join(root, sqliteEntry))
	if err != nil {
		return err
	}
	if activeDigest != "" && !packages[activeDigest] {
		return errors.New("active theme package payload is missing from the backup")
	}
	return nil
}

func archivedActiveThemeDigest(sqlitePath string) (string, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(sqlitePath)+"?mode=ro&_query_only=1")
	if err != nil {
		return "", fmt.Errorf("open archived sqlite read-only: %w", err)
	}
	defer func() { _ = db.Close() }()
	var activeID sql.NullString
	if err := db.QueryRow("SELECT active_theme_package_id FROM site_settings WHERE id = 1").Scan(&activeID); err != nil {
		if strings.Contains(err.Error(), "no such column") || strings.Contains(err.Error(), "no such table") {
			return "", nil
		}
		return "", fmt.Errorf("query archived active theme: %w", err)
	}
	if !activeID.Valid || activeID.String == "" {
		return "", nil
	}
	var digest string
	if err := db.QueryRow("SELECT archive_digest FROM theme_packages WHERE id = ?", activeID.String).Scan(&digest); err != nil {
		return "", nil
	}
	return digest, nil
}

func validateExtractedPayloads(ctx context.Context, root string, manifest model.BackupManifest) error {
	if err := validatePayloads(root, manifest); err != nil {
		return err
	}
	if err := validateGeoIPPayloads(root, manifest); err != nil {
		return err
	}
	if err := validateAgentReleaseSnapshot(ctx, root, manifest); err != nil {
		return err
	}
	if err := validateSQLite(ctx, filepath.Join(root, sqliteEntry)); err != nil {
		return err
	}
	if err := validateOIDCCiphertexts(ctx, filepath.Join(root, sqliteEntry),
		filepath.Join(root, filepath.FromSlash(dataKeyEntry))); err != nil {
		return err
	}
	if _, err := validateTrafficCalibrationLedger(ctx, filepath.Join(root, sqliteEntry)); err != nil {
		return err
	}
	if _, err := validateMetricErasureLedger(ctx, filepath.Join(root, sqliteEntry)); err != nil {
		return err
	}
	return nil
}
