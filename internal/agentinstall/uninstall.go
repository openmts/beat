package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// updateStateDirectory 与 agentupdate 的内部状态目录同名；只有整目录都归安装者所有时才删除。
const updateStateDirectory = ".beat-agent-update"

// managedUnitMarker 是安装器写出的单元文件固定首部，卸载只删除带这个前缀的单元。
const managedUnitMarker = "# Managed by beat-agent"

// purgedStateFiles 是 --purge 允许删除的安装状态文件白名单，目录里的其它文件一律保留。
var purgedStateFiles = []string{"agent.json", "enrollment.json", "installation.json", "identity.json", "discovery.json"}

// ErrNotInstalled 表示本机没有 beat-agent 的服务安装残留，无需卸载。
var ErrNotInstalled = errors.New("beat-agent service is not installed on this machine")

// UninstallOptions 控制卸载范围：缺省只移除服务单元与可执行文件，Purge 时一并清除配置与身份。
type UninstallOptions struct {
	Purge bool
}

// UninstallSummary 记录本次卸载移除与保留的路径，方便运维脚本断言。
type UninstallSummary struct {
	Removed  []string
	Retained []string
}

// Uninstall 停止并移除安装器创建的 systemd 单元与可执行文件。配置和身份默认保留，
// 只有 Purge 才删除，避免一次误操作丢掉需要留档的节点凭据。
//
// 两个阶段共用同一把安装锁：清除配置必须在锁内完成，否则并发的 join/install 可能刚写进
// agent.json 就被删掉，留下半安装状态。
func (installer *Installer) Uninstall(ctx context.Context, options UninstallOptions) (UninstallSummary, error) {
	if err := installer.checkHost(ctx); err != nil {
		return UninstallSummary{}, err
	}
	if err := installer.requireInstalled(); err != nil {
		return UninstallSummary{}, err
	}
	release, err := installer.uninstallLock()
	if err != nil {
		return UninstallSummary{}, err
	}
	summary, uninstallErr := installer.uninstallLocked(ctx, options)
	return summary, errors.Join(uninstallErr, release())
}

func (installer *Installer) uninstallLocked(ctx context.Context, options UninstallOptions) (UninstallSummary, error) {
	summary, err := installer.uninstallServices(ctx)
	if err != nil {
		return summary, err
	}
	if !options.Purge {
		summary.Retained = append(summary.Retained, installer.paths.stateDir)
		return summary, nil
	}
	return installer.purgeState(summary)
}

func (installer *Installer) uninstallServices(ctx context.Context) (summary UninstallSummary, err error) {
	if err = installer.stopInstalledServices(ctx); err != nil {
		return summary, err
	}
	units, err := installer.removeInstalledUnits()
	if err != nil {
		return summary, err
	}
	if len(units) != 0 {
		if _, err = installer.systemctl(ctx, "daemon-reload"); err != nil {
			return summary, err
		}
	}
	binaries, err := installer.removeInstalledBinaries()
	if err != nil {
		return summary, err
	}
	updateState, err := installer.removeUpdateState()
	if err != nil {
		return summary, err
	}
	summary.Removed = append(summary.Removed, units...)
	summary.Removed = append(summary.Removed, binaries...)
	summary.Removed = append(summary.Removed, updateState...)
	return summary, nil
}

// requireInstalled 用安装器自己的落地点判断是否存在可卸载的安装，避免对无关机器动手。
// 状态目录也算：先 uninstall 再 uninstall --purge 时，剩下的只有 /etc/beat-agent。
func (installer *Installer) requireInstalled() error {
	for _, path := range []string{installer.paths.unit, installer.helperUnitPath(),
		installer.paths.binary, installer.paths.stateDir} {
		_, err := os.Lstat(path)
		if err == nil {
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	return fmt.Errorf("%w: run beat-agent join or service install first", ErrNotInstalled)
}

// uninstallLock 与 install/update 共用同一把安装锁；状态目录不存在时说明没有并发安装，直接放行。
func (installer *Installer) uninstallLock() (func() error, error) {
	info, err := os.Lstat(installer.paths.stateDir)
	if os.IsNotExist(err) {
		return func() error { return nil }, nil
	}
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("unsafe agent state directory %s", installer.paths.stateDir)
	}
	return acquireLock(filepath.Join(installer.paths.stateDir, "installation.lock"))
}

// stopInstalledServices 先停再禁用；systemd 里查不到的单元跳过，其余失败都中止卸载。
func (installer *Installer) stopInstalledServices(ctx context.Context) error {
	for _, name := range []string{ServiceName, UpdateServiceName} {
		output, err := installer.systemctl(ctx, "show", name,
			"--property=FragmentPath", "--property=DropInPaths", "--property=LoadState", "--no-pager")
		if err != nil {
			if !missingSystemdUnit(output) {
				return fmt.Errorf("inspect %s before uninstall: %w", name, err)
			}
			continue
		}
		status := systemdProperties(output)
		if len(status) != systemdPropertyCount {
			return errors.New("cannot uninstall with unknown systemd overrides")
		}
		if status["LoadState"] == "not-found" {
			continue
		}
		if _, err = installer.systemctl(ctx, "disable", "--now", name); err != nil {
			return err
		}
	}
	return nil
}

func (installer *Installer) removeInstalledUnits() ([]string, error) {
	var removed []string
	for _, path := range []string{installer.paths.unit, installer.helperUnitPath()} {
		gone, err := removeManagedUnit(path)
		if err != nil {
			return removed, err
		}
		if gone {
			removed = append(removed, path)
		}
	}
	return removed, nil
}

// removeManagedUnit 只删除首部带安装器标记的单元；遇到别人写的同名单元宁可失败也不覆盖。
func removeManagedUnit(path string) (bool, error) {
	data, err := readPrivate(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.HasPrefix(string(data), managedUnitMarker) {
		return false, fmt.Errorf("refusing to remove %s: it is not managed by beat-agent", path)
	}
	if err = os.Remove(path); err != nil {
		return false, err
	}
	return true, syncDirectory(filepath.Dir(path))
}

func (installer *Installer) removeInstalledBinaries() ([]string, error) {
	var removed []string
	for _, path := range []string{installer.paths.binary, installer.helperBinaryPath()} {
		if err := checkFile(path, 0o755); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return removed, err
		}
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed = append(removed, path)
	}
	if gone, err := removeDirectoryIfEmpty(filepath.Dir(installer.paths.binary)); err != nil {
		return removed, err
	} else if gone {
		removed = append(removed, filepath.Dir(installer.paths.binary))
	}
	return removed, nil
}

// removeUpdateState 删除 agentupdate 的整块状态目录，它只包含已签名镜像的回执与回滚状态。
func (installer *Installer) removeUpdateState() ([]string, error) {
	path := filepath.Join(filepath.Dir(installer.paths.binary), updateStateDirectory)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || !ownedByCurrentUser(info) || info.Mode().Perm()&^os.FileMode(privateDirectory) != 0 {
		return nil, fmt.Errorf("unsafe update state directory %s", path)
	}
	if err = os.RemoveAll(path); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func (installer *Installer) purgeState(summary UninstallSummary) (UninstallSummary, error) {
	for _, name := range purgedStateFiles {
		path := filepath.Join(installer.paths.stateDir, name)
		if err := checkFile(path, privateFile); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return summary, err
		}
		if err := os.Remove(path); err != nil {
			return summary, err
		}
		summary.Removed = append(summary.Removed, path)
	}
	lockPath := filepath.Join(installer.paths.stateDir, "installation.lock")
	if _, err := os.Lstat(lockPath); err == nil {
		if err := os.Remove(lockPath); err != nil {
			return summary, err
		}
		summary.Removed = append(summary.Removed, lockPath)
	} else if !os.IsNotExist(err) {
		return summary, err
	}
	gone, err := removeDirectoryIfEmpty(installer.paths.stateDir)
	if err != nil {
		return summary, err
	}
	if gone {
		summary.Removed = append(summary.Removed, installer.paths.stateDir)
		return summary, nil
	}
	summary.Retained = append(summary.Retained, installer.paths.stateDir)
	return summary, nil
}

func removeDirectoryIfEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) != 0 {
		return false, nil
	}
	if err = os.Remove(path); err != nil {
		return false, err
	}
	return true, syncDirectory(filepath.Dir(path))
}
