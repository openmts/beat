package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/agentjoin"
)

func unitFile(binary, config string) []byte {
	return []byte("# Managed by beat-agent service install (v1).\n" +
		"[Unit]\nDescription=Beat monitoring agent\nWants=network-online.target\nAfter=network-online.target\n\n" +
		"[Service]\nType=simple\nUser=root\nUMask=0077\nExecStart=:" +
		systemdQuote(binary) + " --config " + systemdQuote(config) +
		"\nRestart=on-failure\nRestartSec=5s\n\n[Install]\nWantedBy=multi-user.target\n")
}

// ExecStart 的 : 前缀禁用环境变量替换；百分号仍按 systemd 规则转义。
func systemdQuote(value string) string {
	var escaped strings.Builder
	_ = escaped.WriteByte('"')
	for _, character := range []byte(value) {
		switch character {
		case '\\', '"':
			_ = escaped.WriteByte('\\')
			_ = escaped.WriteByte(character)
		case '%':
			_ = escaped.WriteByte(character)
			_ = escaped.WriteByte(character)
		default:
			if character < ' ' || character == 0x7f {
				_, _ = fmt.Fprintf(&escaped, "\\x%02x", character)
			} else {
				_ = escaped.WriteByte(character)
			}
		}
	}
	_ = escaped.WriteByte('"')
	return escaped.String()
}

func (installer *Installer) startService(ctx context.Context, unit []byte) error {
	if err := installer.write(installer.paths.unit, unit, privateFile); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if _, err := installer.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if _, err := installer.systemctl(ctx, "enable", "--now", ServiceName); err != nil {
		return err
	}
	if _, err := installer.systemctl(ctx, "is-active", "--quiet", ServiceName); err != nil {
		return fmt.Errorf("service is not active; inspect systemctl status %s: %w", ServiceName, err)
	}
	return nil
}

func (installer *Installer) Install(ctx context.Context, configPath string) (err error) {
	if configPath == "" {
		configPath = installer.paths.config
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return err
	}
	release, err := installer.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	if err := installer.loadServiceConfig(configPath); err != nil {
		return err
	}
	unit := unitFile(installer.paths.binary, configPath)
	if err := installer.checkSystemd(ctx, unit); err != nil {
		return err
	}
	plan, err := installer.planBinary(configPath)
	if err != nil {
		return err
	}
	if err := installer.installBinary(plan); err != nil {
		return err
	}
	return installer.startService(ctx, unit)
}

func (installer *Installer) checkConfig(state *enrollment) error {
	existing, err := readPrivate(installer.paths.config)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if state == nil || state.Result == nil {
		return errors.New("an existing agent config was found; use service install --config to manage it without joining again")
	}
	var config agentjoin.Config
	if err := decodeJSON(strings.NewReader(string(existing)), &config); err != nil {
		return err
	}
	if config != state.Result.Config {
		return errors.New("existing agent config differs from enrollment; it will not be overwritten")
	}
	return nil
}
