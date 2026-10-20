package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/applicationsecrets"
)

func acquireServerLease(dbPath string) (*applicationsecrets.Lease, error) {
	dataDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(filepath.Dir(dataDir), 0o700); err != nil {
		return nil, err
	}
	lease, err := applicationsecrets.AcquireLease(dataDir)
	if err != nil {
		return nil, fmt.Errorf("create data directory lifecycle lease: %w", err)
	}
	return lease, nil
}

func handleSecretCommand(args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "secrets" {
		return false, nil
	}
	if len(args) < 2 || (args[1] != "rotate-root" && args[1] != "lock-path") {
		return true, errors.New("usage: beat-server secrets {rotate-root|lock-path} --data-dir DIR [--db-path FILE]")
	}
	flags := flag.NewFlagSet("secrets "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("data-dir", "./data", "application data directory")
	database := flags.String("db-path", "", "existing SQLite database path")
	if err := flags.Parse(args[2:]); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, errors.New("unexpected application secret arguments")
	}
	if args[1] == "lock-path" {
		path, err := applicationsecrets.LifecycleLockPath(*directory)
		if err != nil {
			return true, err
		}
		_, err = fmt.Fprintln(output, path)
		return true, err
	}
	if *database == "" {
		*database = filepath.Join(*directory, "beat.db")
	}
	if err := applicationsecrets.RotateRoot(context.Background(), applicationsecrets.RootRotationConfig{
		DataDir: *directory, DatabasePath: *database,
	}); err != nil {
		return true, err
	}
	_, err := fmt.Fprintln(output, "Application root key rotation completed; the server can be started.")
	return true, err
}

type secretReadiness interface{ Ready(context.Context) error }

func (operations *operability) secretsCheck(ctx context.Context) readinessCheck {
	if operations.secrets == nil {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	if err := operations.secrets.Ready(ctx); err != nil {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) notificationCheck(ctx context.Context) readinessCheck {
	if operations.notificationQueue == nil {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	if err := operations.notificationQueue.Ready(ctx); err != nil {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	return readinessCheck{Status: "ok"}
}
