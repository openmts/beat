package agentinstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type installation struct {
	Version      int    `json:"version"`
	ConfigPath   string `json:"config_path"`
	BinarySHA256 string `json:"binary_sha256"`
}

type binaryPlan struct {
	record installation
	data   []byte
}

func (installer *Installer) planBinary(configPath string) (binaryPlan, error) {
	record, err := installer.readInstallation(configPath)
	if err != nil {
		return binaryPlan{}, err
	}
	source, err := installer.executable()
	if err != nil {
		return binaryPlan{}, fmt.Errorf("locate current agent executable: %w", err)
	}
	data, err := readSourceExecutable(source)
	if err != nil {
		return binaryPlan{}, err
	}
	existing, err := readExecutable(installer.paths.binary)
	if os.IsNotExist(err) {
		return binaryPlan{record: installation{stateVersion, configPath, binaryDigest(data)}, data: data}, nil
	}
	if err != nil {
		return binaryPlan{}, err
	}
	return installer.planExistingBinary(record, data, existing)
}

func (installer *Installer) readInstallation(configPath string) (installation, error) {
	var record installation
	found, err := loadPrivateJSON(filepath.Join(installer.paths.stateDir, "installation.json"), &record)
	if err != nil {
		return installation{}, err
	}
	if !found {
		return installation{Version: stateVersion, ConfigPath: configPath}, nil
	}
	if record.Version != stateVersion || record.ConfigPath != configPath {
		return installation{}, errors.New("another service installation is recorded; use its original config path")
	}
	return record, nil
}

func (installer *Installer) planExistingBinary(record installation, source, existing []byte) (binaryPlan, error) {
	if !bytes.Equal(source, existing) && record.BinarySHA256 != binaryDigest(existing) {
		return binaryPlan{}, errors.New("an unmanaged executable already exists at the installation path")
	}
	record.BinarySHA256 = binaryDigest(existing)
	return installer.existingBinaryPlan(record, existing)
}

// 内核的当前进程镜像不受下载目录中的路径替换影响。
func readSourceExecutable(path string) ([]byte, error) {
	if path == "/proc/self/exe" {
		return os.ReadFile(path)
	}
	return readExecutable(path)
}

func readExecutable(path string) ([]byte, error) {
	if err := checkFile(path, 0o755); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o100 == 0 {
		return nil, fmt.Errorf("agent binary %s is not executable by its owner", path)
	}
	return os.ReadFile(path)
}

func binaryDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (installer *Installer) installBinary(plan binaryPlan) error {
	if err := installer.saveJSON(filepath.Join(installer.paths.stateDir, "installation.json"), plan.record); err != nil {
		return err
	}
	if plan.data == nil {
		return nil
	}
	return installer.write(installer.paths.binary, plan.data, executableFile)
}

func (installer *Installer) existingBinaryPlan(record installation, data []byte) (binaryPlan, error) {
	info, err := os.Stat(installer.paths.binary)
	if err != nil {
		return binaryPlan{}, err
	}
	plan := binaryPlan{record: record}
	if info.Mode().Perm() != executableFile {
		plan.data = data
	}
	return plan, nil
}
