//go:build !linux

package remoteoperations

import "os"

func openPrivateSpoolFile(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	return root.Open(name)
}
