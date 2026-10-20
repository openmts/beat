package theme

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

func parseManifest(raw []byte) (model.ThemeManifest, error) {
	var manifest model.ThemeManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return model.ThemeManifest{}, ErrManifestInvalid
	}
	if err := validateManifestMetadata(manifest); err != nil {
		return model.ThemeManifest{}, err
	}
	return manifest, nil
}

func validateManifestReferencedFiles(manifest model.ThemeManifest, inventory map[string]ManifestFile) error {
	seen := make(map[string]bool)
	for _, declared := range manifest.Files {
		if _, ok := inventory[declared.Path]; !ok || seen[declared.Path] {
			return ErrManifestInvalid
		}
		seen[declared.Path] = true
	}
	if _, ok := inventory[manifest.Preview]; !ok {
		return ErrManifestInvalid
	}
	return nil
}

func sandboxEntry(manifest model.ThemeManifest) (string, error) {
	entry := ""
	for _, file := range manifest.Files {
		if strings.EqualFold(filepath.Ext(file.Path), ".html") {
			if entry != "" {
				return "", ErrManifestInvalid
			}
			entry = file.Path
		}
	}
	if entry == "" {
		return "", ErrManifestInvalid
	}
	return entry, nil
}

func validThemeID(id string) bool {
	if len(id) < 3 || len(id) > 64 {
		return false
	}
	for _, character := range id {
		if character > 127 || !isIDCharacter(byte(character)) {
			return false
		}
	}
	return true
}

func isIDCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_'
}

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

func validateManifestMetadata(manifest model.ThemeManifest) error {
	if manifest.SchemaVersion != 1 || !validThemeID(manifest.ID) {
		return ErrManifestInvalid
	}
	for _, value := range []string{manifest.Name, manifest.Description, manifest.Author, manifest.License} {
		if value == "" {
			return ErrManifestInvalid
		}
	}
	if manifest.Runtime != model.ThemeRuntimeDeclarative && manifest.Runtime != model.ThemeRuntimeSandboxed {
		return ErrManifestInvalid
	}
	if err := validateManifestVersions(manifest); err != nil {
		return err
	}
	if _, err := validateEntryPath(manifest.Preview); err != nil {
		return ErrManifestInvalid
	}
	if !strings.HasPrefix(mimeForAsset(manifest.Preview), "image/") {
		return ErrManifestInvalid
	}
	if manifest.Runtime == model.ThemeRuntimeSandboxed && manifest.Configuration != nil {
		return ErrManifestInvalid
	}
	return validateManifestConfiguration(manifest.Configuration)
}

func validateManifestVersions(manifest model.ThemeManifest) error {
	for _, version := range []string{manifest.Version, manifest.MinimumBeatVersion} {
		if !validSemver(version) {
			return ErrManifestInvalid
		}
	}
	if manifest.MaximumBeatVersion != "" && !validSemver(manifest.MaximumBeatVersion) {
		return ErrManifestInvalid
	}
	return nil
}
