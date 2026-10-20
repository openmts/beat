package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const systemdPropertyCount = 3

func (installer *Installer) begin(ctx context.Context) (func() error, error) {
	if err := installer.checkHost(ctx); err != nil {
		return nil, err
	}
	for _, directory := range []string{installer.paths.stateDir, filepath.Dir(installer.paths.binary)} {
		if err := ensureDirectory(directory, true); err != nil {
			return nil, err
		}
	}
	if err := ensureDirectory(filepath.Dir(installer.paths.unit), false); err != nil {
		return nil, err
	}
	if err := probeDirectory(filepath.Dir(installer.paths.unit)); err != nil {
		return nil, fmt.Errorf("systemd unit directory is not writable: %w", err)
	}
	return acquireLock(filepath.Join(installer.paths.stateDir, "installation.lock"))
}

func (installer *Installer) checkHost(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if installer.goos != "linux" || installer.euid() != 0 {
		return errors.New("agent service installation requires Linux and root; run with sudo")
	}
	if info, err := os.Stat(installer.paths.systemd); err != nil || !info.IsDir() {
		return errors.New("systemd is not running on this machine")
	}
	if _, err := installer.lookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl is required: %w", err)
	}
	return nil
}

func (installer *Installer) systemctl(ctx context.Context, args ...string) (string, error) {
	executable, err := installer.lookPath("systemctl")
	if err != nil {
		return "", fmt.Errorf("find systemctl: %w", err)
	}
	return installer.run(ctx, executable, append([]string{"--system"}, args...))
}

func (installer *Installer) checkSystemd(ctx context.Context, unit []byte) error {
	existing, err := readPrivate(installer.paths.unit)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && string(existing) != string(unit) {
		return errors.New("an unmanaged beat-agent.service already exists; preserve it and resolve the conflict first")
	}
	output, err := installer.systemctl(ctx, "show", ServiceName,
		"--property=FragmentPath", "--property=DropInPaths", "--property=LoadState", "--no-pager")
	if err != nil && !missingSystemdUnit(output) {
		return fmt.Errorf("cannot query systemd before joining: %w", err)
	}
	return installer.admitSystemdUnit(output, installer.paths.unit)
}

// admitSystemdUnit 在单元身份校验之外单独放通 vendor 目录里只含超时键的系统级 drop-in。
func (installer *Installer) admitSystemdUnit(output, path string) error {
	if err := validateSystemdUnit(output, path); err != nil {
		return err
	}
	return installer.admitDropIns(systemdProperties(output)["DropInPaths"])
}

func missingSystemdUnit(output string) bool {
	status := systemdProperties(output)
	return len(status) == systemdPropertyCount && status["LoadState"] == "not-found" &&
		status["FragmentPath"] == "" && status["DropInPaths"] == ""
}

func systemdProperties(output string) map[string]string {
	status := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "LoadState" && key != "FragmentPath" && key != "DropInPaths" {
			return nil
		}
		if _, exists := status[key]; exists {
			return nil
		}
		status[key] = value
	}
	return status
}

func validateSystemdUnit(output, path string) error {
	status := systemdProperties(output)
	if len(status) != systemdPropertyCount {
		return errors.New("cannot install with unknown systemd overrides")
	}
	if status["LoadState"] != "loaded" && status["LoadState"] != "not-found" {
		return errors.New("cannot install over a masked, failed or unknown systemd unit")
	}
	if status["FragmentPath"] != path && !missingSystemdUnit(output) {
		return errors.New("a beat-agent service from another location already exists")
	}
	return nil
}
