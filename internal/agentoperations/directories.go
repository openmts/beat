package agentoperations

import (
	"errors"
	"os"
)

func createPrivateDirectory(path string) error {
	err := os.Mkdir(path, 0o700)
	if err == nil {
		return secureCreatedDirectory(path)
	}
	if errors.Is(err, os.ErrExist) {
		return privateDirectory(path, true)
	}
	return ErrStorage
}
