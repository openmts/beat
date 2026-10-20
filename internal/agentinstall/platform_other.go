//go:build !linux

package agentinstall

import (
	"errors"
	"os"
)

func ownedByCurrentUser(os.FileInfo) bool { return false }

func acquireLock(string) (func() error, error) {
	return nil, errors.New("agent service installation requires Linux")
}
