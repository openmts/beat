package theme

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/beat/backend/internal/model"
)

const (
	maxArchiveBytes     = 25 << 20
	maxExtracted        = 64 << 20
	maxFiles            = 512
	maxManifestBytes    = 256 << 10
	maxDataFileBytes    = 8 << 20
	maxAssetFileBytes   = 12 << 20
	maxPathBytes        = 240
	maxPathDepth        = 8
	maxCompressionRatio = 100
	manifestName        = "beat-theme.json"
)

var (
	ErrArchiveInvalid      = errors.New("theme: archive is invalid")
	ErrManifestInvalid     = errors.New("theme: manifest is invalid")
	ErrPackageConflict     = errors.New("theme: package identity or digest conflict")
	ErrContentUnavailable  = errors.New("theme: package content is unavailable")
	ErrRuntimeIncompatible = errors.New("theme: runtime is not compatible")
	ErrActivePackage       = errors.New("theme: active package cannot be deleted")
	ErrNoHealthyRollback   = errors.New("theme: no healthy rollback target exists")
	ErrMarketUnavailable   = errors.New("theme: market is unavailable")
	ErrMarketInvalid       = errors.New("theme: market catalog is invalid")
)

type ManifestFile struct {
	Path   string
	Size   int64
	SHA256 string
}

type ValidatedPackage struct {
	Digest        string
	ArchiveBytes  int64
	ContentBytes  int64
	Manifest      model.ThemeManifest
	Files         []ManifestFile
	ManifestBytes []byte
	Content       map[string][]byte
}

func ValidateArchive(raw []byte) (*ValidatedPackage, error) {
	if len(raw) == 0 || len(raw) > maxArchiveBytes {
		return nil, ErrArchiveInvalid
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ZIP", ErrArchiveInvalid)
	}
	manifestFile, err := inspectArchive(reader)
	if err != nil {
		return nil, err
	}
	manifestBytes, err := readZipEntry(manifestFile, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, err := parseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	validated := &ValidatedPackage{Digest: sha256Hex(raw), ArchiveBytes: int64(len(raw)), Manifest: manifest,
		ManifestBytes: manifestBytes, Content: make(map[string][]byte)}
	if err := collectArchiveContent(reader, validated); err != nil {
		return nil, err
	}
	if err := validateBundle(validated); err != nil {
		return nil, err
	}
	return validated, nil
}

func collectArchiveContent(reader *zip.Reader, validated *ValidatedPackage) error {
	inventory := make(map[string]ManifestFile)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Name == manifestName {
			continue
		}
		content, err := readZipEntry(file, maxAssetFileBytes)
		if err != nil {
			return err
		}
		if _, err := validateAssetContent(file.Name, content); err != nil {
			return err
		}
		validated.Content[file.Name] = content
		entry := ManifestFile{Path: file.Name, Size: int64(len(content)), SHA256: sha256Hex(content)}
		inventory[file.Name] = entry
		validated.ContentBytes += entry.Size
	}
	if len(validated.Manifest.Files) != len(inventory) {
		return ErrManifestInvalid
	}
	if err := validateManifestReferencedFiles(validated.Manifest, inventory); err != nil {
		return err
	}
	for _, declared := range validated.Manifest.Files {
		actual := inventory[declared.Path]
		if actual.Size != declared.Size || actual.SHA256 != declared.SHA256 {
			return ErrManifestInvalid
		}
		validated.Files = append(validated.Files, actual)
	}
	sort.Slice(validated.Files, func(i, j int) bool { return validated.Files[i].Path < validated.Files[j].Path })
	return nil
}

func readZipEntry(file *zip.File, limit int) ([]byte, error) {
	input, err := file.Open()
	if err != nil {
		return nil, ErrArchiveInvalid
	}
	defer func() { _ = input.Close() }()
	content, err := io.ReadAll(io.LimitReader(input, int64(limit)+1))
	if err != nil || len(content) > limit {
		return nil, ErrArchiveInvalid
	}
	return content, nil
}
