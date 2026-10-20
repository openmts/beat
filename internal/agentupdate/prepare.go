package agentupdate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func (u *Updater) begin(inst *installation, verified verifiedAssignment) (*journal, error) {
	if err := cleanupJournalTemporary(inst); err != nil {
		return nil, err
	}
	existing, err := readJournal(inst)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err = u.validateExisting(existing, verified.assignment); err != nil {
			return nil, err
		}
		if !terminalPhase(existing.Phase) {
			return existing, nil
		}
	}

	if err = removeStaging(inst); err != nil {
		return nil, err
	}
	value := &journal{Version: journalVersion, ServerURL: u.config.ServerURL, Executable: u.config.Executable, Assignment: verified.assignment,
		Previous: u.config.Installed, TargetVersion: verified.manifest.Version, VerifiedAt: verified.verifiedAt, Phase: "downloading"}
	if err = saveJournal(inst, value); err != nil {
		return nil, err
	}
	return value, nil
}

func sameAssignment(left, right protocol.Assignment) bool {
	return left.ServerSchema == right.ServerSchema && left.Action == right.Action && left.ExpiresAt.Equal(right.ExpiresAt) && bytes.Equal(left.Manifest.Signature, right.Manifest.Signature) && left.ID == right.ID && left.RolloutID == right.RolloutID && left.NodeID == right.NodeID && left.Nonce == right.Nonce && left.Artifact == right.Artifact && protocol.ManifestDigest(left.Manifest.Payload) == protocol.ManifestDigest(right.Manifest.Payload)
}

func terminalPhase(phase string) bool {
	return phase == "healthy" || phase == "rolled_back" || phase == "canceled"
}

func removeStaging(inst *installation) error {
	for _, name := range []string{downloadFile, rollbackFile, "previous.next"} {
		file, err := inst.state.open(name, os.O_RDONLY, 0)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		if err = inst.state.remove(name); err != nil {
			return err
		}
	}
	return inst.state.sync()
}

func (u *Updater) prepare(ctx context.Context, inst *installation, value *journal) error {
	current, err := inst.root.open(inst.name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = current.Close() }()
	if err = imageVerifier(u.config.Arch).verifyImage(ctx, current, value.Previous); err != nil {
		return ErrConflict
	}
	if err = (verifiedSource{current, value.Previous}).replacePrevious(ctx, inst, u.config.Arch); err != nil {
		return err
	}
	if err = u.checkpoint("previous_synced"); err != nil {
		return err
	}
	candidate, err := inst.state.open(downloadFile, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = imageVerifier(u.config.Arch).makeExecutable(ctx, candidate, imageFor(value))
	closeErr := candidate.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	value.Phase = "prepared"
	if err = saveJournal(inst, value); err != nil {
		return err
	}
	return u.checkpoint("prepared")
}

type verifiedSource struct {
	file  *os.File
	image Image
}

func (source verifiedSource) replacePrevious(ctx context.Context, inst *installation, arch string) error {
	current, image := source.file, source.image
	if err := safeRemove(inst.state, "previous.next"); err != nil {
		return err
	}
	if _, err := current.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := privateCopy(io.LimitReader(&contextReader{ctx: ctx, reader: current}, image.Size+1), inst.state, "previous.next"); err != nil {
		return err
	}
	previous, err := inst.state.open("previous.next", os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	verifyErr := imageVerifier(arch).verifyImage(ctx, previous, image)
	if err = errors.Join(verifyErr, previous.Close()); err != nil {
		return err
	}
	if err = renameRelative(fileRef{inst.state, "previous.next"}, fileRef{inst.state, previousFile}); err != nil {
		return err
	}
	return inst.state.sync()
}

func (checker imageVerifier) makeExecutable(ctx context.Context, file *os.File, image Image) error {
	if err := checker.verifyImage(ctx, file, image); err != nil {
		return err
	}
	if err := file.Chmod(0o700); err != nil {
		return err
	}
	return file.Sync()
}

func safeRemove(dir *directory, name string) error {
	file, err := dir.open(name, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return dir.remove(name)
}

func (u *Updater) validateExisting(existing *journal, assignment protocol.Assignment) error {
	if err := u.validateJournal(existing); err != nil {
		return err
	}
	if !terminalPhase(existing.Phase) && !sameAssignment(existing.Assignment, assignment) {
		return ErrBusy
	}
	return nil
}
