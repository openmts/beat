package agentupdate

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

const pendingAssignmentFile = "assignment.json"

const trustFile = "trust.json"

const helperProofFile = "helper.json"

// Settings只由本地安装CLI修改；省略或Enabled=false时不轮询、不启动更新。
type Settings struct {
	Enabled bool   `json:"enabled"`
	NodeID  string `json:"node_id"`
}

func StoreTrust(executable string, keys []protocol.SigningTrust) error {
	inst, err := openInstallation(executable, true)
	if err != nil {
		return err
	}
	defer inst.close()
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	if len(keys) == 0 || len(data) > maximumJournalBytes {
		return ErrConfiguration
	}
	if err = safeRemove(inst.state, trustFile+".tmp"); err != nil {
		return err
	}
	return writePrivate(inst.state, trustFile, data)
}

func LoadTrust(executable string) ([]protocol.SigningTrust, error) {
	inst, err := openInstallation(executable, false)
	if err != nil {
		return nil, err
	}
	defer inst.close()
	if inst.state == nil {
		return nil, ErrManualOnly
	}
	data, err := readPrivate(inst.state, trustFile, maximumJournalBytes)
	if err != nil {
		return nil, err
	}
	var keys []protocol.SigningTrust
	if err = decodePrivateJSON(data, &keys); err != nil {
		return nil, err
	}
	return cloneTrust(keys), nil
}

func (u *Updater) Enqueue(assignment protocol.Assignment) error {
	if err := u.admitEnqueue(assignment); err != nil {
		return err
	}
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return err
	}
	defer inst.close()
	if inst.state == nil {
		return ErrManualOnly
	}
	current, err := readJournal(inst)
	if err != nil {
		return err
	}
	if activeJournal(current) {
		return ErrBusy
	}
	data, err := json.Marshal(assignment)
	if err != nil || len(data) > maximumJournalBytes {
		return ErrAssignment
	}
	if err = safeRemove(inst.state, pendingAssignmentFile+".tmp"); err != nil {
		return err
	}
	return writePrivate(inst.state, pendingAssignmentFile, data)
}

func (u *Updater) Pending() (*protocol.Assignment, error) {
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return nil, err
	}
	defer inst.close()
	if inst.state == nil {
		return nil, nil
	}
	data, err := readPrivate(inst.state, pendingAssignmentFile, maximumJournalBytes)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var assignment protocol.Assignment
	if err = decodePrivateJSON(data, &assignment); err != nil {
		return nil, err
	}
	current, err := readJournal(inst)
	if err != nil {
		return nil, err
	}
	if current != nil && sameAssignment(current.Assignment, assignment) && !prepublication(current.Phase) {
		return nil, nil
	}
	return &assignment, nil
}

func (u *Updater) Observation(at time.Time) (*protocol.Observation, error) {
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return nil, err
	}
	defer inst.close()
	value, err := readJournal(inst)
	if err != nil || value == nil {
		return nil, err
	}
	if err = u.validateJournal(value); err != nil {
		return nil, err
	}
	outcome := ""
	switch value.Phase {
	case "healthy":
		outcome = "healthy"
	case "rolled_back":
		outcome = "rolled_back"
	}
	if outcome == "" {
		return nil, nil
	}
	version := value.TargetVersion
	if outcome == "rolled_back" {
		version = value.Previous.Version
	}
	return &protocol.Observation{AssignmentID: value.Assignment.ID, Nonce: value.Assignment.Nonce, Version: version, ReportAt: at, Outcome: outcome, Restarts: value.Restarts}, nil
}

// VerifyRunningImage验证内核当前进程镜像，不以可被rename的路径冒充正在运行版本。
func (u *Updater) VerifyRunningImage(ctx context.Context) error {
	if u.config.OS != "linux" {
		return ErrManualOnly
	}
	file, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return imageVerifier(u.config.Arch).verifyImage(ctx, file, u.config.Installed)
}

func ValidateSettings(settings Settings, server string) error {
	if !settings.Enabled {
		return nil
	}
	if !protocol.ValidSigningKeyID(settings.NodeID) {
		return ErrConfiguration
	}
	_, err := trustedServer(server)
	return err
}

func activeJournal(value *journal) bool { return value != nil && !terminalPhase(value.Phase) }

func (u *Updater) admitEnqueue(assignment protocol.Assignment) error {
	if !u.config.Enabled {
		return ErrDisabled
	}
	if err := u.managementGate(); err != nil {
		return err
	}
	if _, err := u.verify(assignment, u.config.Now()); err != nil {
		return err
	}

	return nil
}
