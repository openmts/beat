package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/testutil"
)

type archiveEntry struct {
	name string
	kind byte
	body []byte
}

func archiveBytes(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	writer := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: 0o600, Size: int64(len(entry.body))}); err != nil {
			t.Fatal(err)
		}
		if len(entry.body) > 0 {
			if _, err := writer.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestArchiveRejectsUnsafeEntries(t *testing.T) {
	database := testutil.CountryMMDB("US", 1700000000)
	cases := [][]archiveEntry{
		{{"../country.mmdb", tar.TypeReg, database}}, {{"/country.mmdb", tar.TypeReg, database}}, {{"dir\\country.mmdb", tar.TypeReg, database}},
		{{"country.mmdb", tar.TypeSymlink, nil}}, {{"country.mmdb", tar.TypeLink, nil}}, {{"country.mmdb", tar.TypeChar, nil}},
		{{"country.mmdb", tar.TypeReg, database}, {"country.mmdb", tar.TypeReg, database}},
		{{"a.mmdb", tar.TypeReg, database}, {"b.mmdb", tar.TypeReg, database}}, {{"README", tar.TypeReg, []byte("none")}},
		{{"country.mmdb", tar.TypeReg, bytes.Repeat([]byte{0}, 1024*1024)}},
	}
	for _, entries := range cases {
		t.Run(entries[0].name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "archive.tar.gz")
			if err := os.WriteFile(archive, archiveBytes(t, entries), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := extractDatabase(archive, dir); err != ErrInvalid {
				t.Fatal(err)
			}
		})
	}
}

func TestArchiveExtractionAndDatabaseValidation(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive.tar.gz")
	content := archiveBytes(t, []archiveEntry{{"country/", tar.TypeDir, nil}, {"country/LICENSE", tar.TypeReg, []byte("synthetic")}, {"country/Country.mmdb", tar.TypeReg, testutil.CountryMMDB("US", 1700000000)}})
	if err := os.WriteFile(archive, content, 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := extractDatabase(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	reader, metadata, err := validateDatabase(path)
	if err != nil || metadata.SizeBytes == 0 {
		t.Fatal(metadata, err)
	}
	database := &Database{reader: reader}
	t.Cleanup(func() { _ = database.Close() })
	if country, err := database.Lookup("8.8.8.8"); err != nil || country != "US" {
		t.Fatal(country, err)
	}
	if _, err = database.Lookup("10.0.0.1"); err != ErrInvalid {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Lookup("8.8.8.8"); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestArchiveRejectsCorruptFooterAndTrailingStream(t *testing.T) {
	valid := archiveBytes(t, []archiveEntry{{"country.mmdb", tar.TypeReg, testutil.CountryMMDB("US", 1700000000)}})
	corrupt := bytes.Clone(valid)
	corrupt[len(corrupt)-8] ^= 0xff
	for _, content := range [][]byte{valid[:len(valid)-4], corrupt, append(bytes.Clone(valid), []byte("trailing")...), append(bytes.Clone(valid), valid...)} {
		dir := t.TempDir()
		path := filepath.Join(dir, "archive.tar.gz")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := extractDatabase(path, dir); err != ErrInvalid {
			t.Fatal("accepted bad gzip framing", err)
		}
	}
}
