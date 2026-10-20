package agentupdate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const journalVersion = 1

type journal struct {
	HealthProcess  string              `json:"health_process,omitempty"`
	Restarts       int                 `json:"restarts"`
	Restarted      bool                `json:"restarted"`
	Version        int                 `json:"version"`
	ServerURL      string              `json:"server_url"`
	Executable     string              `json:"executable"`
	Assignment     protocol.Assignment `json:"assignment"`
	Previous       Image               `json:"previous"`
	TargetVersion  string              `json:"target_version"`
	VerifiedAt     time.Time           `json:"verified_at"`
	Phase          string              `json:"phase"`
	Reason         string              `json:"reason,omitempty"`
	Downloaded     int64               `json:"downloaded"`
	PublishedAt    time.Time           `json:"published_at"`
	FirstHealthyAt time.Time           `json:"first_healthy_at"`
	LastHealthyAt  time.Time           `json:"last_healthy_at"`
	Deadline       time.Time           `json:"deadline"`
}

func readJournal(inst *installation) (*journal, error) {
	if inst.state == nil {
		return nil, nil
	}
	content, err := readPrivate(inst.state, journalFile, maximumJournalBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var value journal
	if err = decoder.Decode(&value); err != nil {
		return nil, ErrJournal
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, ErrJournal
	}
	if !validJournal(value) {
		return nil, ErrJournal
	}
	return &value, nil
}

func validJournal(value journal) bool {
	if value.Version != journalVersion || !validImage(value.Previous) || value.VerifiedAt.IsZero() {
		return false
	}
	if !validRestartJournal(value) {
		return false
	}
	if value.Downloaded < 0 || value.Downloaded > value.Assignment.Artifact.Size {
		return false
	}
	switch value.Phase {
	case "downloading", "prepared", "published", "observing", "healthy", "rollback_prepared", "rolled_back", "canceled":
		return true
	}
	return false
}

func (u *Updater) validateJournal(value *journal) error {
	if value.Executable != u.config.Executable || !u.validJournalBinding(value) {
		return ErrJournal
	}
	expected, err := trustedServer(u.config.ServerURL)
	if err != nil {
		return err
	}
	actual, err := trustedServer(value.ServerURL)
	if err != nil || actual.String() != expected.String() {
		return ErrJournal
	}
	manifest, err := protocol.DecodeManifest(value.Assignment.Manifest.Payload)
	if err != nil {
		return err
	}
	trust, ok := u.signingTrust(manifest.SigningKeyID)
	if !ok {
		return protocol.ErrSignature
	}
	manifest, err = protocol.VerifyManifestSignature(value.Assignment.Manifest, trust, value.VerifiedAt)
	if err != nil {
		return err
	}
	if !validJournalManifest(value, manifest) {
		return ErrJournal
	}
	return u.compatibleArtifact(manifest, value.Assignment.Artifact)
}

func saveJournal(inst *installation, value *journal) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maximumJournalBytes {
		return ErrJournal
	}
	return writePrivate(inst.state, journalFile, encoded)
}

func statusOf(value *journal) Status {
	if value == nil {
		return Status{Phase: "idle"}
	}
	status := Status{Phase: value.Phase, Reason: value.Reason, AssignmentID: value.Assignment.ID, TargetVersion: value.TargetVersion, PreviousVersion: value.Previous.Version, Downloaded: value.Downloaded}
	if !value.Deadline.IsZero() {
		deadline := value.Deadline
		status.WatchdogDeadline = &deadline
	}
	return status
}

func cleanupJournalTemporary(inst *installation) error {
	if inst.state == nil {
		return nil
	}
	file, err := inst.state.open(journalFile+".tmp", os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return inst.state.remove(journalFile + ".tmp")
}

func sameImage(left, right Image) bool {
	return protocol.SameReleaseVersion(left.Version, right.Version) && left.Size == right.Size && left.SHA256 == right.SHA256
}

func (u *Updater) validJournalBinding(value *journal) bool {
	if !validAssignmentIdentity(value.Assignment, u.config.NodeID, value.VerifiedAt) || value.VerifiedAt.After(u.config.Now()) {
		return false
	}
	if !validDirection(value.Assignment.Action, value.TargetVersion, value.Previous.Version) {
		return false
	}
	if !sameImage(u.config.Installed, value.Previous) && !sameImage(u.config.Installed, imageFor(value)) {
		return false
	}
	return u.validJournalTimes(value)
}

func (u *Updater) validJournalTimes(value *journal) bool {
	if value.PublishedAt.IsZero() {
		return value.Deadline.IsZero() && (value.Phase == "downloading" || value.Phase == "prepared" || value.Phase == "canceled")
	}
	if value.PublishedAt.Before(value.VerifiedAt) || value.PublishedAt.After(u.config.Now()) {
		return false
	}
	duration := value.Deadline.Sub(value.PublishedAt)
	if duration < 2*time.Second || duration > 70*time.Minute {
		return false
	}
	return validJournalHealth(value)
}

func validJournalHealth(value *journal) bool {
	if value.FirstHealthyAt.IsZero() {
		return value.LastHealthyAt.IsZero() && value.Phase != "observing" && value.Phase != "healthy"
	}
	return !value.FirstHealthyAt.Before(value.PublishedAt) && !value.LastHealthyAt.Before(value.FirstHealthyAt) && value.LastHealthyAt.Before(value.Deadline)
}

const maximumLocalFailures = 100000

func validRestartJournal(value journal) bool {
	return value.Restarts >= 0 && value.Restarts <= maximumLocalFailures && (value.HealthProcess == "" || protocol.ValidSigningKeyID(value.HealthProcess))
}

func validJournalManifest(value *journal, manifest protocol.Manifest) bool {
	return manifest.Version == value.TargetVersion && value.Assignment.ServerSchema >= manifest.MinimumServerSchema
}
