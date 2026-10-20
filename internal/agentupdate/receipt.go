package agentupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const installedReceiptFile = "installed.json"

// InstallationProof由带外key验证的完整manifest构成，不接受自报hash作为安装证据。
type InstallationProof struct {
	Manifest   protocol.SignedManifest `json:"manifest"`
	ArtifactID string                  `json:"artifact_id"`
	VerifiedAt time.Time               `json:"verified_at"`
}

type installedReceipt struct {
	Current  InstallationProof  `json:"current"`
	Previous *InstallationProof `json:"previous,omitempty"`
}

type BootstrapOptions struct {
	Config     Config
	Manifest   protocol.SignedManifest
	ArtifactID string
}

func Bootstrap(ctx context.Context, options BootstrapOptions) (Image, error) {
	config := proofConfig(options.Config)
	if err := bootstrapGate(config); err != nil {
		return Image{}, err
	}
	proof := InstallationProof{Manifest: options.Manifest, ArtifactID: options.ArtifactID, VerifiedAt: config.Now()}
	u := &Updater{config: config}
	image, err := u.proofImage(proof)
	if err != nil {
		return Image{}, err
	}
	inst, err := openInstallation(config.Executable, true)
	if err != nil {
		return Image{}, err
	}
	defer inst.close()
	if err = u.verifyCurrent(ctx, inst, image); err != nil {
		return Image{}, err
	}
	existing, err := readPrivate(inst.state, installedReceiptFile, maximumJournalBytes)
	if err == nil {
		var receipt installedReceipt
		if err = decodePrivateJSON(existing, &receipt); err != nil {
			return Image{}, ErrJournal
		}
		previous, err := u.proofImage(receipt.Current)
		if err != nil || !sameImage(previous, image) {
			return Image{}, ErrConflict
		}
		return image, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Image{}, err
	}
	proof.Manifest.SigstoreBundle = nil
	err = saveInstalled(inst, installedReceipt{Current: proof})
	return image, err
}

func (u *Updater) proofImage(proof InstallationProof) (Image, error) {
	now := time.Now().UTC()
	if u.config.Now != nil {
		now = u.config.Now()
	}
	if proof.VerifiedAt.IsZero() || proof.VerifiedAt.After(now) {
		return Image{}, ErrJournal
	}
	manifest, err := protocol.DecodeManifest(proof.Manifest.Payload)
	if err != nil {
		return Image{}, err
	}
	trust, ok := u.signingTrust(manifest.SigningKeyID)
	if !ok {
		return Image{}, protocol.ErrSignature
	}
	manifest, err = protocol.VerifyManifestSignature(proof.Manifest, trust, proof.VerifiedAt)
	if err != nil {
		return Image{}, err
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.ID != proof.ArtifactID {
			continue
		}
		if err = u.compatibleArtifact(manifest, artifact); err != nil {
			return Image{}, err
		}
		return Image{Version: manifest.Version, Size: artifact.Size, SHA256: artifact.SHA256}, nil
	}
	return Image{}, ErrAssignment
}

func saveInstalled(inst *installation, receipt installedReceipt) error {
	encoded, err := json.Marshal(receipt)
	if err != nil || len(encoded) > maximumJournalBytes {
		return ErrJournal
	}
	if err = safeRemove(inst.state, installedReceiptFile+".tmp"); err != nil {
		return err
	}
	return writePrivate(inst.state, installedReceiptFile, encoded)
}

// ResolveInstalled校验当前入口的签名证据，并从durable journal恢复已发布目标。
// 仅healthy后晋升receipt；上一签名证明仍保留供回滚验证。
func ResolveInstalled(ctx context.Context, config Config) (Image, error) {
	config = proofConfig(config)
	inst, err := openInstallation(config.Executable, false)
	if err != nil {
		return Image{}, err
	}
	defer inst.close()
	if inst.state == nil {
		return Image{}, ErrManualOnly
	}
	data, err := readPrivate(inst.state, installedReceiptFile, maximumJournalBytes)
	if err != nil {
		return Image{}, ErrManualOnly
	}
	var receipt installedReceipt
	if err = decodePrivateJSON(data, &receipt); err != nil {
		return Image{}, err
	}
	u := &Updater{config: config}
	anchor, err := u.proofImage(receipt.Current)
	if err != nil {
		return Image{}, err
	}
	u.config.Installed = anchor
	value, err := readJournal(inst)
	if err != nil {
		return Image{}, err
	}
	if value != nil {
		if err = u.validateJournal(value); err != nil {
			return Image{}, err
		}
		return u.resolveJournalImage(ctx, transaction{inst, value}, receipt)
	}
	if err = u.verifyCurrent(ctx, inst, anchor); err != nil {
		return Image{}, err
	}
	return anchor, nil
}

func (u *Updater) resolveJournalImage(ctx context.Context, txn transaction, receipt installedReceipt) (Image, error) {
	state, err := u.currentState(ctx, txn.inst, txn.value)
	if err != nil {
		return Image{}, err
	}
	if state == "previous" {
		return u.restoreReceipt(txn.inst, txn.value, receipt)
	}

	target := imageFor(txn.value)
	if txn.value.Phase != "healthy" || sameImage(u.config.Installed, target) {
		return target, nil
	}
	proof := InstallationProof{Manifest: txn.value.Assignment.Manifest, ArtifactID: txn.value.Assignment.Artifact.ID, VerifiedAt: txn.value.VerifiedAt}
	previous := receipt.Current
	receipt.Previous = &previous
	receipt.Current = proof
	if err = saveInstalled(txn.inst, receipt); err != nil {
		return Image{}, err
	}
	return target, nil
}

func (u *Updater) restoreReceipt(inst *installation, value *journal, receipt installedReceipt) (Image, error) {
	var err error
	if !sameImage(u.config.Installed, value.Previous) {
		if receipt.Previous == nil {
			return Image{}, ErrJournal
		}
		previous, verifyErr := u.proofImage(*receipt.Previous)
		if verifyErr != nil || !sameImage(previous, value.Previous) {
			return Image{}, ErrJournal
		}
		receipt.Current, receipt.Previous = *receipt.Previous, nil
		if err = saveInstalled(inst, receipt); err != nil {
			return Image{}, err
		}
	}
	return value.Previous, nil
}

func proofConfig(config Config) Config {
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return config
}

func bootstrapGate(config Config) error {
	if config.Container {
		return ErrExternalManager
	}
	if config.OS != "linux" || (config.Arch != "amd64" && config.Arch != "arm64") {
		return ErrManualOnly
	}
	return nil
}
