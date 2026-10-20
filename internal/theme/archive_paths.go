package theme

import (
	"archive/zip"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func inspectArchive(reader *zip.Reader) (*zip.File, error) {
	if len(reader.File) > maxFiles {
		return nil, ErrArchiveInvalid
	}
	var manifest *zip.File
	var total uint64
	seen := make(map[string]bool)
	for _, file := range reader.File {
		name, err := inspectZipEntry(file)
		if err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		canonical := strings.ToLower(name)
		if seen[canonical] {
			return nil, ErrArchiveInvalid
		}
		seen[canonical] = true
		total += file.UncompressedSize64
		if total > maxExtracted {
			return nil, ErrArchiveInvalid
		}
		if name == manifestName {
			manifest = file
			continue
		}
	}
	if manifest == nil {
		return nil, ErrManifestInvalid
	}
	return manifest, nil
}

func validateZipSize(file *zip.File) error {
	if file.UncompressedSize64 > maxAssetFileBytes {
		return ErrArchiveInvalid
	}
	if file.CompressedSize64 > 0 && (file.UncompressedSize64+maxCompressionRatio-1)/maxCompressionRatio > file.CompressedSize64 {
		return ErrArchiveInvalid
	}
	return nil
}

func validateEntryPath(raw string) (string, error) {
	if raw == "" || len(raw) > maxPathBytes || !utf8.ValidString(raw) {
		return "", ErrArchiveInvalid
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f {
			return "", ErrArchiveInvalid
		}
	}
	if strings.ContainsAny(raw, "\\:") || strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) {
		return "", ErrArchiveInvalid
	}
	segments := strings.Split(raw, "/")
	if len(segments) > maxPathDepth {
		return "", ErrArchiveInvalid
	}
	for _, segment := range segments {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", ErrArchiveInvalid
		}
	}
	return raw, nil
}

func validateContentType(path string) error {
	if mimeForAsset(path) == "" {
		return ErrArchiveInvalid
	}
	return nil
}

func inspectZipEntry(file *zip.File) (string, error) {
	name := strings.TrimSuffix(file.Name, "/")
	if _, err := validateEntryPath(name); err != nil {
		return "", err
	}
	if file.FileInfo().IsDir() {
		return name, nil
	}
	if !file.Mode().IsRegular() || file.Flags&1 != 0 {
		return "", ErrArchiveInvalid
	}
	if err := validateZipSize(file); err != nil {
		return "", err
	}
	if name != manifestName {
		if err := validateContentType(name); err != nil {
			return "", err
		}
	}
	return name, nil
}
