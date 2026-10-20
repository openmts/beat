package releasesignature

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTrustFileRequiresPrivateStableRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	want := []byte(`{"trusted":true}`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTrustFile(path)
	if err != nil || !bytes.Equal(want, got) {
		t.Fatal("trust content mismatch", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTrustFile(path); err != nil {
		t.Fatal("read-only private trust rejected", err)
	}
	for _, permission := range []os.FileMode{0o644, 0o700, 0o200, 0o000} {
		if err := os.Chmod(path, permission); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadTrustFile(path); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("unsafe mode %o accepted: %v", permission, err)
		}
	}
}

func TestReadTrustFileRejectsPathsAndSizeBoundaries(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "trust.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "relative", directory, path + ".missing", link, directory + "/../" + filepath.Base(directory) + "/trust.json"} {
		if _, err := ReadTrustFile(bad); !errors.Is(err, ErrConfiguration) {
			t.Fatal("unsafe path accepted", bad, err)
		}
	}
	for _, size := range []int{0, maximumRootBytes, maximumRootBytes + 1} {
		if err := os.WriteFile(path, bytes.Repeat([]byte{'a'}, size), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ReadTrustFile(path)
		if (err == nil) != (size == maximumRootBytes) {
			t.Fatal("wrong trust size boundary", size, err)
		}
	}
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTrustFile(path); !errors.Is(err, ErrConfiguration) {
		t.Fatal("untrusted parent accepted", err)
	}
}
