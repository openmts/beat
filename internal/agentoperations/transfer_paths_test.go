//go:build linux

package agentoperations

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRootHandlesRejectTraversalLinksAndNonRegularFiles(t *testing.T) {
	directory := privateTestDir(t)
	outside := filepath.Join(privateTestDir(t), "secret")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openTransferRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(directory, "hard")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(directory, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", "link", "hard", "fifo", "directory"} {
		if file, err := root.openSource(path); err == nil {
			_ = file.Close()
			t.Fatalf("unsafe source accepted: %s", path)
		}
	}
	device := filepath.Join(directory, "device")
	if err := unix.Mknod(device, unix.S_IFCHR|0o600, int(unix.Mkdev(1, 3))); err == nil {
		if file, err := root.openSource("device"); err == nil {
			_ = file.Close()
			t.Fatal("device opened as source")
		}
	}
	if _, err := root.destination("link/child"); err == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestOpenedSourceRemainsBoundToItsOriginalFile(t *testing.T) {
	directory := privateTestDir(t)
	if err := os.WriteFile(filepath.Join(directory, "source"), []byte("authorized"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openTransferRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	source, err := root.openSource("source")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if err := os.Rename(filepath.Join(directory, "source"), filepath.Join(directory, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(directory, "source")); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(source)
	if err != nil || string(content) != "authorized" {
		t.Fatal("opened source followed a replacement path")
	}
}

func TestTransferRejectsNestedMounts(t *testing.T) {
	directory := privateTestDir(t)
	nested := filepath.Join(directory, "mount")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("tmpfs", nested, "tmpfs", unix.MS_NODEV|unix.MS_NOSUID, "size=1m,mode=0700"); err != nil {
		t.Skipf("isolated mount fixture unavailable: %v", err)
	}
	defer func() {
		if err := unix.Unmount(nested, 0); err != nil {
			t.Error(err)
		}
	}()
	if err := os.WriteFile(filepath.Join(nested, "source"), []byte("mounted"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openTransferRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if _, err := root.openSource("mount/source"); err == nil {
		t.Fatal("cross-mount source accepted")
	}
	if _, err := root.destination("mount/target"); err == nil {
		t.Fatal("cross-mount destination accepted")
	}
}

func TestTemporaryOperationsRejectUnownedOrUnrelatedTargets(t *testing.T) {
	directory := privateTestDir(t)
	root, err := openTransferRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	destination, err := root.destination("target")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = destination.Close() }()
	if _, err := destination.temporary("../outside", true); err == nil {
		t.Fatal("unsafe temporary path accepted")
	}
	if err := destination.removeTemporary("../outside"); err == nil {
		t.Fatal("unsafe cleanup accepted")
	}
	if err := destination.publish("../outside", false); err == nil {
		t.Fatal("unsafe publication accepted")
	}
	name, err := temporaryName()
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.removeTemporary(name); err != nil {
		t.Fatal(err)
	}
	file, err := destination.temporary(name, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("new")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "target"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := destination.publish(name, false); !errors.Is(err, ErrFileConflict) {
		t.Fatal("implicit overwrite accepted")
	}
	if err := destination.removeTemporary(name); err != nil {
		t.Fatal(err)
	}
}
