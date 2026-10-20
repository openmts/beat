//go:build linux

package agentoperations

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestTransferSuperMagicKeeps32BitSignExtension(t *testing.T) {
	for _, name := range []string{"EXT4", "XFS", "BTRFS", "TMPFS", "OVERLAYFS", "RAMFS", "F2FS"} {
		magic := transferSuperMagic(transferTestMagic(t, name))
		if got := transferSuperMagic(int64(int32(magic))); got != magic {
			t.Fatalf("%s magic lost sign extension: %d != %d", name, got, magic)
		}
	}
	if transferSuperMagic(unix.BTRFS_SUPER_MAGIC) != uint32(unix.BTRFS_SUPER_MAGIC) {
		t.Fatal("btrfs magic must keep its 32-bit pattern")
	}
}

func transferTestMagic(t *testing.T, name string) int64 {
	t.Helper()
	values := map[string]int64{
		"EXT4": unix.EXT4_SUPER_MAGIC, "XFS": unix.XFS_SUPER_MAGIC, "BTRFS": unix.BTRFS_SUPER_MAGIC,
		"TMPFS": unix.TMPFS_MAGIC, "OVERLAYFS": unix.OVERLAYFS_SUPER_MAGIC, "RAMFS": unix.RAMFS_MAGIC, "F2FS": unix.F2FS_SUPER_MAGIC,
	}
	value, ok := values[name]
	if !ok {
		t.Fatalf("unknown magic %q", name)
	}
	return value
}
