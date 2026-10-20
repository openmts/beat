package agentupdate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
)

const HelperFilename = "beat-agent-update-helper"

func InstallHelper(ctx context.Context, config Config) error {
	u := &Updater{config: config}
	inst, err := openInstallation(config.Executable, false)
	if err != nil {
		return err
	}
	defer inst.close()
	data, err := readPrivate(inst.state, installedReceiptFile, maximumJournalBytes)
	if err != nil {
		return err
	}
	var receipt installedReceipt
	if err = decodePrivateJSON(data, &receipt); err != nil {
		return err
	}
	image, err := u.proofImage(receipt.Current)
	if err != nil {
		return err
	}
	if err = u.verifyCurrent(ctx, inst, image); err != nil {
		return err
	}
	if err = u.admitHelper(ctx, inst, image); err != nil {
		return err
	}
	if err = u.copyHelper(ctx, inst, image); err != nil {
		return err
	}
	encoded, err := json.Marshal(receipt.Current)
	if err != nil {
		return err
	}
	if err = safeRemove(inst.state, helperProofFile+".tmp"); err != nil {
		return err
	}
	return writePrivate(inst.state, helperProofFile, encoded)
}

func (u *Updater) copyHelper(ctx context.Context, inst *installation, image Image) error {
	source, err := inst.root.open(inst.name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	const temporary = "beat-agent-update-helper.next"
	if err = safeRemove(inst.root, temporary); err != nil {
		return err
	}
	if err = privateCopy(io.LimitReader(&contextReader{ctx: ctx, reader: source}, image.Size+1), inst.root, temporary); err != nil {
		return err
	}
	ready, err := inst.root.open(temporary, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = imageVerifier(u.config.Arch).makeExecutable(ctx, ready, image)
	if err = errors.Join(err, ready.Close()); err != nil {
		return err
	}
	if err = renameRelative(fileRef{inst.root, temporary}, fileRef{inst.root, HelperFilename}); err != nil {
		return err
	}
	return inst.root.sync()
}

func VerifyHelper(ctx context.Context, config Config) error { return checkHelper(ctx, config, true) }

func CheckHelper(ctx context.Context, config Config) error { return checkHelper(ctx, config, false) }

func checkHelper(ctx context.Context, config Config, running bool) error {
	inst, err := openInstallation(config.Executable, false)
	if err != nil {
		return err
	}
	defer inst.close()
	if inst.state == nil {
		return ErrManualOnly
	}
	data, err := readPrivate(inst.state, helperProofFile, maximumJournalBytes)
	if err != nil {
		return err
	}
	var proof InstallationProof
	if err = decodePrivateJSON(data, &proof); err != nil {
		return err
	}
	u := &Updater{config: config}
	image, err := u.proofImage(proof)
	if err != nil {
		return err
	}
	if running {
		u.config.Installed = image
		return u.VerifyRunningImage(ctx)
	}
	file, err := inst.root.open(HelperFilename, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return imageVerifier(u.config.Arch).verifyImage(ctx, file, image)
}

func (u *Updater) admitHelper(ctx context.Context, inst *installation, image Image) error {
	file, err := inst.root.open(HelperFilename, os.O_RDONLY, 0)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err = imageVerifier(u.config.Arch).verifyImage(ctx, file, image); err == nil {
		return nil
	}
	data, err := readPrivate(inst.state, helperProofFile, maximumJournalBytes)
	if err != nil {
		return ErrConflict
	}
	var proof InstallationProof
	if err = decodePrivateJSON(data, &proof); err != nil {
		return err
	}
	previous, err := u.proofImage(proof)
	if err != nil {
		return err
	}
	return imageVerifier(u.config.Arch).verifyImage(ctx, file, previous)
}
