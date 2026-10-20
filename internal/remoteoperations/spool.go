package remoteoperations

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/beat/backend/internal/secretbox"
)

const MaximumSpoolBytes int64 = 4 * 1024 * 1024 * 1024

const SpoolChunkBytes = secretbox.OperationFrameBytes

type Scanner interface {
	Scan(context.Context, io.Reader) error
}

type SpoolConfig struct {
	Directory    string
	MaximumBytes int64
	Scanner      Scanner
}

type Spool struct {
	root         *os.Root
	maximumBytes int64
	scanner      Scanner
}

func OpenSpool(config SpoolConfig) (*Spool, error) {
	if config.Directory == "" {
		return nil, ErrInvalid
	}
	if config.MaximumBytes == 0 {
		config.MaximumBytes = MaximumSpoolBytes
	}
	if config.MaximumBytes < 1 || config.MaximumBytes > MaximumSpoolBytes {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(config.Directory, 0o700); err != nil {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(config.Directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(config.Directory)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Spool{root: root, maximumBytes: config.MaximumBytes, scanner: config.Scanner}, nil
}

func (spool *Spool) Close() error { return spool.root.Close() }

func (spool *Spool) write(name string, sealed []byte) error {
	file, err := spool.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrUnavailable
	}
	_, writeErr := file.Write(sealed)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr, spool.syncDirectory())
}

func (spool *Spool) read(name string) (sealed []byte, resultErr error) {
	file, err := openPrivateSpoolFile(spool.root, name)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > SpoolChunkBytes+128 {
		return nil, ErrUnavailable
	}
	return io.ReadAll(io.LimitReader(file, SpoolChunkBytes+129))
}

func (spool *Spool) remove(name string) error {
	err := spool.root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return spool.syncDirectory()
}

func (spool *Spool) syncDirectory() (resultErr error) {
	directory, err := spool.root.Open(".")
	if err != nil {
		return ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	return directory.Sync()
}
