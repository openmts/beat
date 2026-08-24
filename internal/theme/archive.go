package theme

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/beat/backend/internal/model"
)

// 归档与内容限制（格式版本 1）。
const (
	maxArchiveBytes     = 25 << 20 // 25 MiB
	maxExtracted        = 64 << 20 // 64 MiB
	maxFiles            = 512
	maxManifestBytes    = 256 << 10 // 256 KiB
	maxDataFileBytes    = 8 << 20   // 8 MiB
	maxAssetFileBytes   = 12 << 20  // 12 MiB
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

// ManifestFile 是展开后的清单文件条目。
type ManifestFile struct {
	Path   string
	Size   int64
	SHA256 string
}

// ValidatedPackage 是一次通过校验的主题包。
type ValidatedPackage struct {
	Digest        string
	ArchiveBytes  int64
	ContentBytes  int64
	Manifest      model.ThemeManifest
	Files         []ManifestFile
	ManifestBytes []byte
	Content       map[string][]byte // 路径 -> 内容（受总大小上限约束）
}

// ValidateArchive 读取并严格校验上传的主题包归档（不落盘任何状态）。
func ValidateArchive(raw []byte) (*ValidatedPackage, error) {
	if len(raw) == 0 || len(raw) > maxArchiveBytes {
		return nil, ErrArchiveInvalid
	}
	digest := sha256.Sum256(raw)
	validated := &ValidatedPackage{
		Digest:       hex.EncodeToString(digest[:]),
		ArchiveBytes: int64(len(raw)),
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrArchiveInvalid, err)
	}
	if len(reader.File) > maxFiles {
		return nil, ErrArchiveInvalid
	}

	var manifestIndex = -1
	var total int64
	seen := map[string]bool{}
	lower := map[string]string{}
	for index, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 || file.Mode()&os.ModeSymlink != 0 {
			return nil, ErrArchiveInvalid
		}
		path, err := validateEntryPath(file.Name)
		if err != nil {
			return nil, err
		}
		if file.UncompressedSize64 > uint64(maxAssetFileBytes) {
			return nil, ErrArchiveInvalid
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > maxCompressionRatio {
			return nil, ErrArchiveInvalid
		}
		if seen[path] {
			return nil, ErrArchiveInvalid
		}
		canonical := strings.ToLower(path)
		if prior, ok := lower[canonical]; ok && prior != path {
			return nil, ErrArchiveInvalid
		}
		lower[canonical] = path
		seen[path] = true
		total += int64(file.UncompressedSize64)
		if total > maxExtracted {
			return nil, ErrArchiveInvalid
		}
		if path == manifestName {
			if manifestIndex != -1 {
				return nil, ErrArchiveInvalid
			}
			manifestIndex = index
			continue
		}
		if err := validateContentType(path); err != nil {
			return nil, err
		}
	}

	if manifestIndex == -1 {
		return nil, ErrManifestInvalid
	}
	manifestBytes, err := readZipEntry(reader.File[manifestIndex], maxManifestBytes)
	if err != nil {
		return nil, err
	}
	validated.ManifestBytes = manifestBytes
	manifest, err := parseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	validated.Manifest = manifest

	inventory := map[string]ManifestFile{}
	validated.Content = make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || file.Name == manifestName {
			continue
		}
		path, pathErr := validateEntryPath(file.Name)
		if pathErr != nil {
			return nil, pathErr
		}
		content, readErr := readZipEntry(file, maxAssetFileBytes)
		if readErr != nil {
			return nil, readErr
		}
		validated.Content[path] = content
		entry := ManifestFile{Path: path, Size: int64(len(content)), SHA256: sha256Hex(content)}
		inventory[path] = entry
		validated.ContentBytes += entry.Size
	}

	// 清单文件清单必须与归档实际文件完全一致。
	if len(manifest.Files) != len(inventory) {
		return nil, ErrManifestInvalid
	}
	validated.Files = make([]ManifestFile, 0, len(manifest.Files))
	for _, declared := range manifest.Files {
		actual, ok := inventory[declared.Path]
		if !ok || actual.Size != declared.Size || actual.SHA256 != declared.SHA256 {
			return nil, ErrManifestInvalid
		}
		validated.Files = append(validated.Files, actual)
	}
	sort.Slice(validated.Files, func(i, j int) bool { return validated.Files[i].Path < validated.Files[j].Path })
	if err := validateManifestReferencedFiles(manifest, inventory); err != nil {
		return nil, err
	}
	return validated, nil
}

func readZipEntry(file *zip.File, limit int) ([]byte, error) {
	input, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrArchiveInvalid, file.Name, err)
	}
	defer func() { _ = input.Close() }()
	content, err := io.ReadAll(io.LimitReader(input, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrArchiveInvalid, file.Name, err)
	}
	if len(content) > limit {
		return nil, ErrArchiveInvalid
	}
	return content, nil
}

func validateEntryPath(raw string) (string, error) {
	if raw == "" || len(raw) > maxPathBytes {
		return "", ErrArchiveInvalid
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f {
			return "", ErrArchiveInvalid
		}
	}
	if strings.ContainsRune(raw, '\\') || strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) {
		return "", ErrArchiveInvalid
	}
	if strings.Contains(raw, ":") && len(raw) >= 2 && raw[1] == ':' {
		return "", ErrArchiveInvalid
	}
	segments := strings.Split(raw, "/")
	if len(segments) > maxPathDepth {
		return "", ErrArchiveInvalid
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", ErrArchiveInvalid
		}
	}
	return raw, nil
}

func validateContentType(path string) error {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".json", ".png", ".jpg", ".jpeg", ".webp", ".woff2", ".css", ".js", ".html", ".svg":
		return nil
	default:
		return ErrArchiveInvalid
	}
}

func parseManifest(raw []byte) (model.ThemeManifest, error) {
	var manifest model.ThemeManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return model.ThemeManifest{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if manifest.SchemaVersion != 1 {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if !validThemeID(manifest.ID) {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if manifest.Name == "" || manifest.Description == "" || manifest.Version == "" ||
		manifest.Author == "" || manifest.License == "" || manifest.Preview == "" {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if manifest.Runtime != model.ThemeRuntimeDeclarative && manifest.Runtime != model.ThemeRuntimeSandboxed {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if manifest.MinimumBeatVersion == "" {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if !validSemver(manifest.Version) || !validSemver(manifest.MinimumBeatVersion) ||
		(manifest.MaximumBeatVersion != "" && !validSemver(manifest.MaximumBeatVersion)) {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if _, err := validateEntryPath(manifest.Preview); err != nil {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	return manifest, nil
}

func validateManifestReferencedFiles(manifest model.ThemeManifest, inventory map[string]ManifestFile) error {
	for _, declared := range manifest.Files {
		if _, ok := inventory[declared.Path]; !ok {
			return ErrManifestInvalid
		}
	}
	if _, ok := inventory[manifest.Preview]; !ok {
		return ErrManifestInvalid
	}
	return nil
}

func validThemeID(id string) bool {
	if len(id) < 3 || len(id) > 64 {
		return false
	}
	for _, character := range id {
		if !isIDCharacter(byte(character)) {
			return false
		}
	}
	return true
}

func isIDCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
		character == '-' || character == '_'
}

// validSemver 校验严格的语义版本（主.次.补丁，可含预发布/构建元数据）。
func validSemver(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	core := version
	if index := strings.IndexAny(core, "+-"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
