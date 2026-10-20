//go:build unix

package releasesignature

import (
	"os"
	"syscall"
)

func trustedOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == 0 || uint64(stat.Uid) == uint64(os.Geteuid()))
}
