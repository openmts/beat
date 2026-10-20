package operatorrecovery

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

type commandOptions struct {
	operation, username, archive string
	config                       Config
	fd                           int
}

// Run 不接受 argv/env 密码。诊断只暴露固定阶段，底层 SQL 和私有内容不进入输出。
func Run(args []string, output io.Writer) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if options.operation == "migration-status" {
		return runMigrationStatus(options, output)
	}
	if options.operation == "migration-check" {
		return runMigrationCheck(options, output)
	}
	if options.operation == "stage-restore" {
		if err := CreateEmptyDirectory(options.config.DataDir); err != nil {
			return errors.New("recovery phase=directory result=failed code=private_directory")
		}
	}
	session, err := Open(context.Background(), options.config)
	if err != nil {
		return errors.New("recovery phase=preflight result=failed code=instance_validation; check ownership, private modes, stopped-service lock, schema, integrity and keys")
	}
	return finishCommand(session, options, output)
}

func finishCommand(session *Session, options commandOptions, output io.Writer) (resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, session.Close()) }()
	switch options.operation {
	case "inspect":
		return json.NewEncoder(output).Encode(session.Status)
	case "unlock-owner":
		return runUnlock(session, options, output)
	case "stage-restore":
		if err := session.StageRestore(context.Background(), options.archive); err != nil {
			return errors.New("recovery phase=stage result=failed code=archive_or_pending_restore")
		}
		_, err := fmt.Fprintln(output, "restore_staged: validated archive published; normal startup will apply the restore")
		return err
	}
	return errors.New("unknown recovery operation")
}

func parseOptions(args []string) (commandOptions, error) {
	options := commandOptions{fd: -1}
	if len(args) == 0 {
		return options, errors.New("usage: admin-recovery {inspect|migration-status|migration-check|unlock-owner|stage-restore} --data-dir DIR [--db-path FILE] [--backup FILE|--archive FILE]")
	}
	options.operation = args[0]
	if !knownOperation(options.operation) {
		return options, errors.New("unknown recovery operation")
	}
	flags := flag.NewFlagSet("admin-recovery", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.config.DataDir, "data-dir", "", "private data directory")
	flags.StringVar(&options.config.DatabasePath, "db-path", "", "SQLite database inside data directory")
	if options.operation == "unlock-owner" {
		flags.StringVar(&options.username, "username", "", "exact owner username")
		flags.IntVar(&options.fd, "secret-fd", -1, "explicit input FD; password, repeat, confirmation on separate lines")
	}
	if options.operation == "stage-restore" {
		flags.StringVar(&options.archive, "archive", "", "private archive file")
	}
	if options.operation == "migration-check" {
		flags.StringVar(&options.archive, "backup", "", "private backup archive file")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return options, errors.New("invalid recovery flags; passwords must not be passed as arguments")
	}
	return options, validateOptions(options, flags.NArg())
}

func knownOperation(operation string) bool {
	switch operation {
	case "inspect", "migration-status", "migration-check", "unlock-owner", "stage-restore":
		return true
	default:
		return false
	}
}

func validateOptions(options commandOptions, positional int) error {
	if positional != 0 || options.config.DataDir == "" || options.fd < -1 {
		return errors.New("invalid recovery arguments")
	}
	if (options.operation == "stage-restore" || options.operation == "migration-check") && options.archive == "" {
		return errors.New("archive path is required")
	}
	if options.operation == "unlock-owner" && options.username == "" {
		return errors.New("exact owner username is required")
	}
	return nil
}

func runUnlock(session *Session, options commandOptions, output io.Writer) (resultErr error) {
	owner, err := session.SelectOwner(options.username)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Database: %q\nSite: %q\nSchema: %d\nOwner: %q\nActions: reset password, remove TOTP, enable owner, revoke all sessions and pending identity transactions\nLocal password login: available\nRepeat exactly: %s\n", session.config.DatabasePath, session.Status.Site, session.Status.Schema, owner.Username, session.Confirmation(owner.Username)); err != nil {
		return err
	}
	input, err := openInput(options.fd)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.Close()) }()
	request := UnlockInput{Username: owner.Username}
	if err := collectUnlock(input, output, &request); err != nil {
		return err
	}
	if err := session.Unlock(context.Background(), request); err != nil {
		return errors.New("recovery phase=unlock result=failed code=confirmation_password_or_transaction")
	}
	_, err = fmt.Fprintln(output, "owner_unlocked: password reset; TOTP removed; owner enabled; all sessions and pending identity transactions revoked; recovery audited")
	return err
}
