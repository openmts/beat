package agentinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/beat/backend/internal/agentupdate"
)

const UpdateServiceName = "beat-agent-update.service"

type updateManager struct {
	installer  *Installer
	configPath string
}

// UpdateManager仅绑定安装器固定服务，禁止由assignment传入服务名或命令。
func (installer *Installer) UpdateManager(ctx context.Context, configPath string) (agentupdate.Manager, error) {
	if installer.container != nil && installer.container() {
		return nil, agentupdate.ErrExternalManager
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return nil, agentupdate.ErrManualOnly
	}
	if err := installer.checkHost(ctx); err != nil {
		return nil, agentupdate.ErrManualOnly
	}
	if err := checkFile(configPath, privateFile); err != nil {
		return nil, agentupdate.ErrManualOnly
	}
	manager := &updateManager{installer: installer, configPath: configPath}
	if err := manager.validate(ctx); err != nil {
		return nil, err
	}
	return manager, nil
}

func (*updateManager) Kind() string { return "systemd" }

func (manager *updateManager) validate(ctx context.Context) error {
	installer := manager.installer
	expected := unitFile(installer.paths.binary, manager.configPath)
	actual, err := readPrivate(installer.paths.unit)
	if err != nil || string(actual) != string(expected) {
		return agentupdate.ErrManualOnly
	}
	output, err := installer.systemctl(ctx, "show", ServiceName, "--property=FragmentPath", "--property=DropInPaths", "--property=LoadState", "--no-pager")
	if err != nil {
		return agentupdate.ErrManualOnly
	}
	if err = installer.admitSystemdUnit(output, installer.paths.unit); err != nil || systemdProperties(output)["LoadState"] != "loaded" {
		return agentupdate.ErrManualOnly
	}
	return nil
}

func (manager *updateManager) Restart(ctx context.Context) error {
	if err := manager.validate(ctx); err != nil {
		return err
	}
	_, err := manager.installer.systemctl(ctx, "--no-block", "restart", ServiceName)
	return err
}

func (installer *Installer) helperUnitPath() string {
	if installer.paths.helperUnit != "" {
		return installer.paths.helperUnit
	}
	return filepath.Join(filepath.Dir(installer.paths.unit), UpdateServiceName)
}

func (installer *Installer) helperBinaryPath() string {
	return filepath.Join(filepath.Dir(installer.paths.binary), "beat-agent-update-helper")
}

func helperUnitFile(binary, config string) []byte {
	return []byte("# Managed by beat-agent service enable-updates (v1).\n" +
		"[Unit]\nDescription=Beat independent update watchdog\nWants=network-online.target\nAfter=network-online.target\n\n" +
		"[Service]\nType=simple\nUser=root\nUMask=0077\nExecStart=:" + systemdQuote(binary) + " update-helper --config " + systemdQuote(config) +
		"\nRestart=always\nRestartSec=1s\n\n[Install]\nWantedBy=multi-user.target\n")
}

func (installer *Installer) startUpdateHelper(ctx context.Context, configPath string) error {
	unit, err := installer.admitHelperUnit(ctx, configPath)
	if err != nil {
		return err
	}
	if err = installer.write(installer.helperUnitPath(), unit, privateFile); err != nil {
		return err
	}
	if _, err = installer.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if _, err = installer.systemctl(ctx, "enable", UpdateServiceName); err != nil {
		return err
	}
	if _, err = installer.systemctl(ctx, "restart", UpdateServiceName); err != nil {
		return err
	}
	_, err = installer.systemctl(ctx, "is-active", "--quiet", UpdateServiceName)
	return err
}

func detectUpdateContainer() bool {
	for _, path := range []string{"/.dockerenv", "/run/.containerenv", "/run/systemd/container"} {
		_, err := os.Stat(path)
		if err == nil {
			return true
		}
		if !os.IsNotExist(err) {
			return true
		}
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != "" || os.Getenv("container") != ""
}

func (installer *Installer) admitHelperUnit(ctx context.Context, configPath string) ([]byte, error) {
	unit := helperUnitFile(installer.helperBinaryPath(), configPath)
	existing, err := readPrivate(installer.helperUnitPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && string(existing) != string(unit) {
		return nil, errors.New("unmanaged update helper unit")
	}
	output, queryErr := installer.systemctl(ctx, "show", UpdateServiceName, "--property=FragmentPath", "--property=DropInPaths", "--property=LoadState", "--no-pager")
	if queryErr != nil && !missingSystemdUnit(output) {
		return nil, queryErr
	}
	if err = installer.admitSystemdUnit(output, installer.helperUnitPath()); err != nil {
		return nil, err
	}

	return unit, nil
}

func (manager *updateManager) AutomaticRestarts(ctx context.Context) (int, error) {
	if err := manager.validate(ctx); err != nil {
		return 0, err
	}
	output, err := manager.installer.systemctl(ctx, "show", ServiceName, "--property=NRestarts", "--value", "--no-pager")
	if err != nil {
		return 0, agentupdate.ErrManager
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || count < 0 {
		return 0, agentupdate.ErrManager
	}
	return count, nil
}

func (manager *updateManager) Ready(ctx context.Context) error {
	installer := manager.installer
	if err := manager.validate(ctx); err != nil {
		return err
	}
	expected := helperUnitFile(installer.helperBinaryPath(), manager.configPath)
	actual, err := readPrivate(installer.helperUnitPath())
	if err != nil || string(actual) != string(expected) {
		return agentupdate.ErrManualOnly
	}
	output, err := installer.systemctl(ctx, "show", UpdateServiceName, "--property=FragmentPath", "--property=DropInPaths", "--property=LoadState", "--no-pager")
	if err != nil {
		return agentupdate.ErrManualOnly
	}
	if err = installer.admitSystemdUnit(output, installer.helperUnitPath()); err != nil || systemdProperties(output)["LoadState"] != "loaded" {
		return agentupdate.ErrManualOnly
	}
	if _, err = installer.systemctl(ctx, "is-active", "--quiet", UpdateServiceName); err != nil {
		return agentupdate.ErrManualOnly
	}
	return nil
}
