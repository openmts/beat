package releasesignature

import (
	"io"
	"os"
	"path/filepath"
)

// ReadTrustFile 仅读取带外安装的私有信任文件，调用方不得传入归档或HTTP请求中的路径。
func ReadTrustFile(path string) (content []byte, resultErr error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !trustedDirectoryChain(filepath.Dir(path)) {
		return nil, ErrConfiguration
	}
	before, err := os.Lstat(path)
	if err != nil || !privateTrustFile(before) {
		return nil, ErrConfiguration
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrConfiguration
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = ErrConfiguration
		}
	}()
	opened, err := file.Stat()
	if err != nil || !privateTrustFile(opened) || !os.SameFile(before, opened) {
		return nil, ErrConfiguration
	}
	content, err = io.ReadAll(io.LimitReader(file, maximumRootBytes+1))
	if err != nil || int64(len(content)) != before.Size() {
		return nil, ErrConfiguration
	}
	after, err := os.Lstat(path)
	if err != nil || !privateTrustFile(after) || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, ErrConfiguration
	}
	if !trustedDirectoryChain(filepath.Dir(path)) {
		return nil, ErrConfiguration
	}
	return content, nil
}

func privateTrustFile(info os.FileInfo) bool {
	return info.Mode().IsRegular() && trustedOwner(info) && info.Mode().Perm()&0o177 == 0 &&
		info.Mode().Perm()&0o400 != 0 && info.Size() > 0 && info.Size() <= maximumRootBytes
}
