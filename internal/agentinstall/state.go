package agentinstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentjoin"
)

type enrollment struct {
	Version       int               `json:"version"`
	ServerURL     string            `json:"server_url"`
	JoinHash      string            `json:"join_token_hash"`
	Request       agentjoin.Request `json:"request"`
	Result        *agentjoin.Result `json:"result,omitempty"`
	RenameAllowed bool              `json:"rename_allowed,omitempty"`
}

func (installer *Installer) enrollment(options JoinOptions) (*enrollment, error) {
	var state enrollment
	found, err := loadPrivateJSON(filepath.Join(installer.paths.stateDir, "enrollment.json"), &state)
	if err != nil || !found {
		return nil, err
	}
	if err := installer.resumeEnrollment(&state, options); err != nil {
		return nil, err
	}
	return &state, nil
}

func (installer *Installer) resumeEnrollment(state *enrollment, options JoinOptions) error {
	if state.Version != stateVersion || state.ServerURL != options.ServerURL {
		return errors.New("another enrollment is recorded; rerun the original join command to recover")
	}
	request := state.Request
	if request.AdvertisedHost != options.Host || request.SSHPort != options.SSHPort {
		return errors.New("enrollment options differ; rerun the original join command to recover")
	}
	if err := installer.updatePendingName(state, options.Name); err != nil {
		return err
	}
	if err := state.Request.Validate(); err != nil {
		return errors.New("stored enrollment is invalid; preserve it for recovery")
	}
	if state.Result != nil {
		if err := validateResult(*state); err != nil {
			return err
		}
	}
	return installer.updatePendingToken(state, options.Token)
}

func (installer *Installer) createEnrollment(options JoinOptions) (*enrollment, error) {
	token, err := installer.credential()
	if err != nil {
		return nil, err
	}
	requestID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("generate join request ID: %w", err)
	}
	state := &enrollment{
		Version: stateVersion, ServerURL: options.ServerURL, JoinHash: tokenHash(options.Token),
		Request: agentjoin.Request{RequestID: requestID.String(), AgentToken: token.Plaintext,
			NodeName: options.Name, AdvertisedHost: options.Host, SSHPort: options.SSHPort},
	}
	if err := state.Request.Validate(); err != nil {
		return nil, err
	}
	return state, installer.saveEnrollment(state)
}

func (installer *Installer) saveEnrollment(state *enrollment) error {
	return installer.saveJSON(filepath.Join(installer.paths.stateDir, "enrollment.json"), state)
}

func (installer *Installer) saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return installer.write(path, append(data, '\n'), privateFile)
}

func loadPrivateJSON(path string, value any) (bool, error) {
	data, err := readPrivate(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := decodeJSON(bytes.NewReader(data), value); err != nil {
		return false, fmt.Errorf("invalid installation state %s: %w", path, err)
	}
	return true, nil
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func validateResult(state enrollment) error {
	result := state.Result
	if result == nil || !validNodeID(result.NodeID) {
		return errors.New("join response contains no node ID")
	}
	config, request := result.Config, state.Request
	if config.ServerURL != state.ServerURL || config.AgentToken != request.AgentToken {
		return errors.New("join response does not match the requested server and node credentials")
	}
	validation := request
	validation.AdvertisedHost, validation.NodeName, validation.SSHPort = config.AdvertisedHost, config.NodeName, config.SSHPort
	if err := validation.Validate(); err != nil || config.AdvertisedHost == "" {
		return errors.New("join response contains invalid node metadata")
	}
	interval, err := time.ParseDuration(config.ReportInterval)
	if err != nil || interval < time.Second {
		return errors.New("join response contains an invalid report interval")
	}
	return nil
}

func (installer *Installer) updatePendingName(state *enrollment, name string) error {
	if state.Request.NodeName == name {
		return nil
	}
	if !state.RenameAllowed || state.Result != nil {
		return errors.New("enrollment name differs; rerun the original command, or change --name only after a name-conflict response")
	}
	state.Request.NodeName, state.RenameAllowed = name, false
	if err := state.Request.Validate(); err != nil {
		return err
	}
	return installer.saveEnrollment(state)
}

func (installer *Installer) updatePendingToken(state *enrollment, token string) error {
	digest := tokenHash(token)
	if state.JoinHash == digest {
		return nil
	}
	if state.Result != nil {
		return errors.New("node enrollment is complete; use the original join command or service install to recover the service")
	}
	state.JoinHash = digest
	return installer.saveEnrollment(state)
}

func validNodeID(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= agentjoin.MaximumRequestIDBytes &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
