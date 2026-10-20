package agentinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentjoin"
)

func (installer *Installer) loadServiceConfig(path string) error {
	_, err := readPrivate(path)
	if os.IsNotExist(err) && path == installer.paths.config {
		if err := installer.restoreConfig(); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("read existing agent config: %w", err)
	}
	_, err = agent.LoadConfig(path)
	return err
}

func (installer *Installer) restoreConfig() error {
	var state enrollment
	found, err := loadPrivateJSON(filepath.Join(installer.paths.stateDir, "enrollment.json"), &state)
	if err != nil {
		return err
	}
	if !found || state.Version != stateVersion || state.Result == nil {
		return errors.New("agent config is missing and no complete enrollment receipt is available")
	}
	if err := validateReceipt(state); err != nil {
		return fmt.Errorf("saved enrollment receipt cannot restore the config: %w", err)
	}
	if err := installer.saveJSON(installer.paths.config, state.Result.Config); err != nil {
		return fmt.Errorf("restore agent config from saved enrollment: %w", err)
	}
	return nil
}

func validateReceipt(state enrollment) error {
	digest, err := hex.DecodeString(state.JoinHash)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("invalid enrollment token digest")
	}
	origin, err := agentjoin.NormalizeServerURL(state.ServerURL, true)
	if err != nil || origin != state.ServerURL {
		return errors.New("invalid enrollment server origin")
	}
	if err := state.Request.Validate(); err != nil {
		return err
	}
	return validateResult(state)
}
