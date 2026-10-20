package releasesignature

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (v *Verifier) Verify(ctx context.Context, evidence Evidence) (resultErr error) {
	if err := v.validateEvidence(ctx, evidence); err != nil {
		return err
	}
	identity, err := v.identity(evidence)
	if err != nil {
		return err
	}
	select {
	case v.slots <- struct{}{}:
	default:
		return ErrBusy
	}
	defer func() { <-v.slots }()
	root, err := v.prepare(evidence)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, removeWorkspace(root)) }()
	return v.run(ctx, root, identity)
}

func (v *Verifier) validateEvidence(ctx context.Context, evidence Evidence) error {
	if v == nil || v.slots == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(evidence.Payload) == 0 || len(evidence.Payload) > MaximumPayloadBytes || len(evidence.Bundle) == 0 || len(evidence.Bundle) > MaximumBundleBytes {
		return ErrRejected
	}
	return nil
}

func (v *Verifier) prepare(evidence Evidence) (root string, resultErr error) {
	if err := v.checkExecutable(); err != nil {
		return "", err
	}
	if !trustedDirectoryChain(v.config.TemporaryDirectory) {
		return "", ErrUnavailable
	}
	root, err := os.MkdirTemp(v.config.TemporaryDirectory, ".beat-signature-")
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, removeWorkspace(root))
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		return root, ErrUnavailable
	}
	if err := writeEvidence(root, evidence, v.config.TrustedRoot); err != nil {
		return root, ErrUnavailable
	}
	return root, nil
}

func removeWorkspace(root string) error {
	if err := os.RemoveAll(root); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (v *Verifier) checkExecutable() error {
	if !trustedDirectoryChain(filepath.Dir(v.config.Executable)) {
		return ErrUnavailable
	}
	current, err := os.Lstat(v.config.Executable)
	if err != nil || !trustedOwner(current) || !sameExecutableSnapshot(v.executable, current) || current.Mode().Perm()&0o022 != 0 || current.Mode().Perm()&0o111 == 0 {
		return ErrUnavailable
	}
	return nil
}

func writeEvidence(root string, evidence Evidence, trustedRoot []byte) error {
	for _, input := range []struct {
		name string
		data []byte
	}{{"payload", evidence.Payload}, {"bundle.json", evidence.Bundle}, {"trusted-root.json", trustedRoot}} {
		if err := writePrivate(filepath.Join(root, input.name), input.data); err != nil {
			return err
		}
	}
	return nil
}

func writePrivate(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func (v *Verifier) run(ctx context.Context, root, identity string) error {
	ctx, cancel := context.WithTimeout(ctx, v.config.Timeout)
	defer cancel()
	command := exec.CommandContext(ctx, v.config.Executable, v.arguments(root, identity)...)
	command.Dir = root
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	command.WaitDelay = time.Second
	output := &boundedOutput{remaining: maximumOutputBytes, cancel: cancel}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	if output.exceeded() {
		return ErrOutputLimit
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return ErrRejected
	}
	return nil
}

func (v *Verifier) arguments(root, identity string) []string {
	return []string{"verify-blob", "--offline", "--new-bundle-format", "--trusted-root", filepath.Join(root, "trusted-root.json"),
		"--bundle", filepath.Join(root, "bundle.json"), "--certificate-identity", identity,
		"--certificate-oidc-issuer", v.config.OIDCIssuer, filepath.Join(root, "payload")}
}
