package releasesignature

import (
	"os"
	"path/filepath"
)

// trustedDirectoryChain 拒绝可被其他本机用户替换的验证程序或临时目录路径。
func trustedDirectoryChain(path string) bool {
	for {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || !trustedOwner(info) {
			return false
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return true
		}
		path = parent
	}
}
