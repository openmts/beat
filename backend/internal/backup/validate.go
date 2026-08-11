package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

const (
	maximumArchiveEntries = 10
	maximumMetricRows     = 1_000_000_000
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
	allowed := map[string]bool{manifestEntry: true, sqliteEntry: true, metricsEntry: true,
		dataKeyEntry: true, checksumsEntry: true}
	seen := map[string]bool{}
	var expanded uint64
	for _, file := range files {
		name := file.Name
		if !allowed[name] || seen[name] || filepath.IsAbs(name) || filepath.Clean(name) != name {
			return fmt.Errorf("backup archive entry %q is not allowed", name)
		}
		seen[name] = true
		if file.FileInfo().Mode()&os.ModeSymlink != 0 || file.FileInfo().IsDir() || file.Flags&1 != 0 {
			return fmt.Errorf("backup archive entry %q has an unsafe type", name)
		}
		if file.Method != zip.Store && file.Method != zip.Deflate {
			return fmt.Errorf("backup archive entry %q uses unsupported compression", name)
		}
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
	if err := validatePayloads(root, manifest); err != nil {
		return model.BackupManifest{}, err
	}
	if err := validateSQLite(ctx, filepath.Join(root, sqliteEntry)); err != nil {
		return model.BackupManifest{}, err
	}
	rows, err := validateMetrics(filepath.Join(root, metricsEntry), manifest.SnapshotCutoff.UnixNano())
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

func readManifest(path string) (model.BackupManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.BackupManifest{}, fmt.Errorf("open backup manifest: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var manifest model.BackupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return model.BackupManifest{}, fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.FormatVersion != 1 || manifest.CreatedAt.IsZero() || manifest.SnapshotCutoff.IsZero() ||
		manifest.MetricRows < 0 || manifest.MetricRows > maximumMetricRows {
		return model.BackupManifest{}, errors.New("backup manifest is incompatible")
	}
	return manifest, nil
}

func validatePayloads(root string, manifest model.BackupManifest) error {
	required := map[string]bool{sqliteEntry: true, metricsEntry: true}
	for name := range manifest.Checksums {
		if name != sqliteEntry && name != metricsEntry && name != dataKeyEntry {
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
	}
	if _, ok := manifest.Checksums[dataKeyEntry]; ok {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(dataKeyEntry)))
		if err != nil || info.Size() != 32 {
			return errors.New("administrator data key is invalid")
		}
	}
	return nil
}

func validateChecksumList(root string, manifest model.BackupManifest) error {
	content, err := os.ReadFile(filepath.Join(root, checksumsEntry))
	if err != nil {
		return fmt.Errorf("read checksum list: %w", err)
	}
	if len(content) > 64<<10 {
		return errors.New("checksum list is too large")
	}
	parsed := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || parsed[parts[1]] != "" {
			return errors.New("checksum list is malformed")
		}
		parsed[parts[1]] = parts[0]
	}
	if len(parsed) != len(manifest.Checksums) {
		return errors.New("checksum list does not match manifest")
	}
	for name, checksum := range manifest.Checksums {
		if parsed[name] != checksum {
			return errors.New("checksum list does not match manifest")
		}
	}
	return nil
}
