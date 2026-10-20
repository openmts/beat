package applicationsecrets

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLifecycleLeaseSurvivesDataDirectorySwap(t *testing.T) {
	parent := t.TempDir()
	data := filepath.Join(parent, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(parent, fmt.Sprintf(".beat-lifecycle-%x.lock", sha256.Sum256([]byte(data))))
	path, err := LifecycleLockPath(data)
	if err != nil || path != want {
		t.Fatalf("lock path = %q, %v", path, err)
	}
	lease, err := AcquireLease(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	if _, err := AcquireLease(data); err == nil {
		t.Fatal("accepted a second lifecycle writer")
	}
	if err := os.Rename(data, data+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLease(data); err == nil {
		t.Fatal("swap bypassed the lifecycle lease")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := AcquireLease(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleLeaseRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "mode", "directory", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			data := filepath.Join(t.TempDir(), "data")
			path, err := LifecycleLockPath(data)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				err = os.Symlink(filepath.Join(filepath.Dir(data), "target"), path)
			case "directory":
				err = os.Mkdir(path, 0o700)
			default:
				err = os.WriteFile(path, nil, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "mode" {
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "hardlink" {
				if err := os.Link(path, path+".alias"); err != nil {
					t.Fatal(err)
				}
			}
			if lease, err := AcquireLease(data); err == nil {
				_ = lease.Close()
				t.Fatal("accepted unsafe lease file")
			}
		})
	}
}

func TestLifecycleLeaseCanonicalPath(t *testing.T) {
	parent := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	want, err := LifecycleLockPath(filepath.Join(parent, "data"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := LifecycleLockPath(filepath.Join(alias, "data"))
	if err != nil || want != got {
		t.Fatalf("alias lock = %q, %v; want %q", got, err, want)
	}
	if _, err := LifecycleLockPath(""); err == nil {
		t.Fatal("accepted empty path")
	}
}
