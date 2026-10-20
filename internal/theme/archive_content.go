package theme

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

const maxImagePixels = 16 * 1024 * 1024

func mimeForAsset(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".html":
		return "text/html"
	default:
		return ""
	}
}

func validateAssetContent(name string, content []byte) (string, error) {
	mime := mimeForAsset(name)
	if mime == "" || len(content) > maxAssetFileBytes {
		return "", ErrArchiveInvalid
	}
	if strings.HasPrefix(mime, "image/") {
		return mime, validateImage(mime, content)
	}
	if mime == "font/woff2" {
		return mime, validateFont(content)
	}
	if len(content) > maxDataFileBytes || !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return "", ErrArchiveInvalid
	}
	if mime == "application/json" && !json.Valid(content) {
		return "", ErrArchiveInvalid
	}
	return mime, nil
}

func validateImage(mime string, content []byte) error {
	if http.DetectContentType(content) != mime {
		return ErrArchiveInvalid
	}
	if mime == "image/webp" {
		return validateWebP(content)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return ErrArchiveInvalid
	}
	if int64(config.Width)*int64(config.Height) > maxImagePixels {
		return ErrArchiveInvalid
	}
	return nil
}

func validateFont(content []byte) error {
	if len(content) < 48 || string(content[:4]) != "wOF2" || uint64(binary.BigEndian.Uint32(content[8:12])) != uint64(len(content)) {
		return ErrArchiveInvalid
	}
	if binary.BigEndian.Uint16(content[12:14]) == 0 || binary.BigEndian.Uint32(content[16:20]) > maxExtracted || binary.BigEndian.Uint32(content[20:24]) > uint32(len(content)) {
		return ErrArchiveInvalid
	}
	return nil
}

func validateBundle(pkg *ValidatedPackage) error {
	if pkg.Manifest.Runtime == model.ThemeRuntimeSandboxed {
		entry, err := sandboxEntry(pkg.Manifest)
		if err != nil {
			return err
		}
		return validateHTML(entry, pkg.Content)
	}
	for path := range pkg.Content {
		mime := mimeForAsset(path)
		if mime == "text/html" || mime == "application/javascript" {
			return ErrManifestInvalid
		}
	}
	definition, ok := pkg.Content[definitionName]
	if !ok {
		return ErrManifestInvalid
	}
	_, err := ParseDefinition(definition)
	return err
}
