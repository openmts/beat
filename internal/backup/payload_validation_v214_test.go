package backup

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func writePayloadFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create payload directory: %v", err)
	}
	writeTestFile(t, path, content)
}

func payloadManifest(t *testing.T, root string, names ...string) model.BackupManifest {
	t.Helper()
	manifest := model.BackupManifest{FormatVersion: 1, Checksums: map[string]string{}, PayloadSizes: map[string]int64{}}
	for _, name := range names {
		size, checksum, err := fileDigest(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("digest %s: %v", name, err)
		}
		manifest.Checksums[name], manifest.PayloadSizes[name] = checksum, size
	}
	return manifest
}

func sqlitePayload(t *testing.T, path string, statements ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create snapshot directory: %v", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("seed archived snapshot (%s): %v", statement, err)
		}
	}
}

func themePayloadManifest(t *testing.T, root string, names []string, themeNames []string) model.BackupManifest {
	t.Helper()
	manifest := payloadManifest(t, root, names...)
	manifest.FormatVersion = 2
	manifest.ThemeFiles = map[string]model.BackupThemeFile{}
	for _, name := range themeNames {
		manifest.ThemeFiles[name] = model.BackupThemeFile{
			Path: name, Size: manifest.PayloadSizes[name], SHA256: manifest.Checksums[name]}
	}
	return manifest
}

func TestValidatePayloadsRejectsThemeMetadataMismatch(t *testing.T) {
	root := t.TempDir()
	digest := strings.Repeat("b", 64)
	themeName := "themes/" + digest + "/archive.zip"
	for _, name := range []string{sqliteEntry, metricsEntry, themeName} {
		writePayloadFile(t, filepath.Join(root, filepath.FromSlash(name)), name)
	}
	manifest := payloadManifest(t, root, sqliteEntry, metricsEntry, themeName)
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("theme payload without manifest metadata accepted")
	}
}

func TestValidatePayloadsRejectsShortAdministratorKey(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{sqliteEntry, metricsEntry, dataKeyEntry} {
		writePayloadFile(t, filepath.Join(root, filepath.FromSlash(name)), name)
	}
	manifest := payloadManifest(t, root, sqliteEntry, metricsEntry, dataKeyEntry)
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("short administrator data key accepted")
	}
}

func TestValidateThemePayloadsRejectsMissingPackageArchive(t *testing.T) {
	root := t.TempDir()
	complete := strings.Repeat("c", 64)
	partial := strings.Repeat("d", 64)
	archive := "themes/" + complete + "/archive.zip"
	payload := "themes/" + partial + "/payload.bin"
	names := []string{sqliteEntry, metricsEntry, archive, payload}
	for _, name := range names {
		writePayloadFile(t, filepath.Join(root, filepath.FromSlash(name)), name)
	}
	manifest := themePayloadManifest(t, root, names, []string{archive, payload})
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("theme package without an archive accepted")
	}
}

func TestValidateThemePayloadsRejectsUnreadableActiveTheme(t *testing.T) {
	root := t.TempDir()
	digest := strings.Repeat("e", 64)
	archive := "themes/" + digest + "/archive.zip"
	names := []string{sqliteEntry, metricsEntry, archive}
	writePayloadFile(t, filepath.Join(root, filepath.FromSlash(archive)), archive)
	writePayloadFile(t, filepath.Join(root, filepath.FromSlash(metricsEntry)), metricsEntry)
	writePayloadFile(t, filepath.Join(root, filepath.FromSlash(sqliteEntry)), "not a database")
	manifest := themePayloadManifest(t, root, names, []string{archive})
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("unreadable archived snapshot accepted")
	}
}

func TestValidateThemePayloadsGracefullySkipsMissingThemeTables(t *testing.T) {
	cases := map[string][]string{
		"missing site settings": {"CREATE TABLE unrelated(id TEXT)"},
		"missing theme packages": {"CREATE TABLE site_settings(id INTEGER, active_theme_package_id TEXT)",
			"INSERT INTO site_settings(id, active_theme_package_id) VALUES(1, 'package')"},
	}
	for name, statements := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			digest := strings.Repeat("f", 64)
			archive := "themes/" + digest + "/archive.zip"
			names := []string{sqliteEntry, metricsEntry, archive}
			writePayloadFile(t, filepath.Join(root, filepath.FromSlash(archive)), archive)
			writePayloadFile(t, filepath.Join(root, filepath.FromSlash(metricsEntry)), metricsEntry)
			sqlitePayload(t, filepath.Join(root, filepath.FromSlash(sqliteEntry)), statements...)
			manifest := themePayloadManifest(t, root, names, []string{archive})
			if err := validatePayloads(root, manifest); err != nil {
				t.Fatalf("legacy snapshot rejected: %v", err)
			}
		})
	}
}

func TestValidateThemePayloadsRejectsMissingActivePackage(t *testing.T) {
	root := t.TempDir()
	digest := strings.Repeat("a", 64)
	archive := "themes/" + digest + "/archive.zip"
	names := []string{sqliteEntry, metricsEntry, archive}
	writePayloadFile(t, filepath.Join(root, filepath.FromSlash(archive)), archive)
	writePayloadFile(t, filepath.Join(root, filepath.FromSlash(metricsEntry)), metricsEntry)
	sqlitePayload(t, filepath.Join(root, filepath.FromSlash(sqliteEntry)),
		"CREATE TABLE site_settings(id INTEGER, active_theme_package_id TEXT)",
		"INSERT INTO site_settings(id, active_theme_package_id) VALUES(1, 'package')",
		"CREATE TABLE theme_packages(id TEXT, archive_digest TEXT)",
		"INSERT INTO theme_packages(id, archive_digest) VALUES('package', '"+strings.Repeat("9", 64)+"')")
	manifest := themePayloadManifest(t, root, names, []string{archive})
	if err := validatePayloads(root, manifest); err == nil {
		t.Fatal("active theme package missing from payloads accepted")
	}
}

func TestValidateExtractedPayloadsRejectsMissingGeoIPAsset(t *testing.T) {
	root := t.TempDir()
	writePayloadFile(t, filepath.Join(root, filepath.FromSlash(metricsEntry)), metricsEntry)
	sqlitePayload(t, filepath.Join(root, filepath.FromSlash(sqliteEntry)), "CREATE TABLE unrelated(id TEXT)")
	manifest := payloadManifest(t, root, sqliteEntry, metricsEntry)
	manifest.FormatVersion = 4
	manifest.GeoIPFiles = map[string]model.MMDBMetadata{geoIPCurrentEntry: {Edition: "test", SHA256: strings.Repeat("1", 64)}}
	if err := validateExtractedPayloads(t.Context(), root, manifest); err == nil {
		t.Fatal("missing managed MMDB payload accepted")
	}
}
