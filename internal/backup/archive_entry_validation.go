package backup

import (
	"archive/zip"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/geoip"
)

func validateArchiveEntry(file *zip.File, seen map[string]bool) error {
	name := file.Name
	if !safeArchiveName(name) || seen[name] {
		return fmt.Errorf("backup archive entry %q is not allowed", name)
	}
	if strings.HasPrefix(name, "themes/") && !validThemeBackupPath(name) {
		return fmt.Errorf("backup archive theme entry %q is not allowed", name)
	}
	return validateArchiveEntryMetadata(file)
}

func safeArchiveName(name string) bool {
	if filepath.IsAbs(name) || filepath.Clean(name) != name {
		return false
	}
	switch name {
	case manifestEntry, sqliteEntry, metricsEntry, dataKeyEntry, checksumsEntry:
		return true
	}
	return strings.HasPrefix(name, "themes/") || validGeoIPEntry(name)
}

func validateArchiveEntryMetadata(file *zip.File) error {
	if file.Name == manifestEntry && file.UncompressedSize64 > maximumManifestBytes {
		return errors.New("backup manifest exceeds its limit")
	}
	if validGeoIPEntry(file.Name) && file.UncompressedSize64 > geoip.MaximumMMDBBytes {
		return errors.New("MMDB archive asset exceeds limit")
	}
	if !file.FileInfo().Mode().IsRegular() || file.Flags&1 != 0 {
		return fmt.Errorf("backup archive entry %q has an unsafe type", file.Name)
	}
	if file.Method != zip.Store && file.Method != zip.Deflate {
		return fmt.Errorf("backup archive entry %q uses unsupported compression", file.Name)
	}
	return nil
}
