package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (service *Service) copyDataKey(temporary string) error {
	source, err := os.Open(service.dataKeyPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open administrator data key: %w", err)
	}
	defer func() { _ = source.Close() }()
	keyDirectory := filepath.Join(temporary, "secrets")
	if err := os.Mkdir(keyDirectory, 0o700); err != nil {
		return fmt.Errorf("create backup secrets directory: %w", err)
	}
	destination, err := os.OpenFile(filepath.Join(keyDirectory, "admin-data.key"),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup administrator key: %w", err)
	}
	_, copyErr := io.Copy(destination, io.LimitReader(source, 33))
	syncErr := destination.Sync()
	closeErr := destination.Close()
	if err := errorsJoin(copyErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("copy administrator data key: %w", err)
	}
	info, err := os.Stat(filepath.Join(keyDirectory, "admin-data.key"))
	if err != nil || info.Size() != 32 {
		return fmt.Errorf("administrator data key must contain exactly 32 bytes")
	}
	return nil
}
