package agentinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxDropInFile = 16 * 1024

// vendorDropInRoots 只包含发行版包管理器负责的 systemd 配置目录。
var vendorDropInRoots = []string{
	"/usr/lib/systemd/system",
	"/usr/local/lib/systemd/system",
	"/lib/systemd/system",
}

var stopFailureModes = map[string]bool{"abort": true, "kill": true, "exit": true}

var timeSpanUnits = map[string]bool{"": true, "s": true, "sec": true, "us": true, "ms": true,
	"min": true, "m": true, "h": true, "d": true, "w": true, "M": true, "y": true}

// dropInKeys 只放通超时相关键；Exec*/User/Group/Environment*/Restart* 等仍按未知覆盖拒绝。
var dropInKeys = map[string]func(string) bool{
	"TimeoutStopFailureMode": validStopFailureMode,
	"TimeoutStopSec":         validTimeSpan,
	"TimeoutAbortSec":        validTimeSpan,
}

func (installer *Installer) admitDropIns(list string) error {
	for _, path := range strings.Fields(list) {
		if err := installer.admitDropIn(path); err != nil {
			return err
		}
	}
	return nil
}

func (installer *Installer) admitDropIn(path string) error {
	if err := checkVendorDropInPath(path, installer.dropInRoots); err != nil {
		return err
	}
	data, err := readDropIn(path)
	if err != nil {
		return err
	}
	return validateDropInKeys(string(data))
}

func checkVendorDropInPath(path string, roots []string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(path) || clean != path || !strings.HasSuffix(clean, ".conf") ||
		!strings.HasSuffix(filepath.Dir(clean), ".d") {
		return errors.New("cannot install with unknown systemd overrides")
	}
	for _, root := range roots {
		if strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return nil
		}
	}
	return errors.New("cannot install with unknown systemd overrides")
}

func readDropIn(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !ownedByCurrentUser(info) || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("unsafe systemd drop-in %s: require a root-owned regular file without untrusted writes", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxDropInFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDropInFile {
		return nil, fmt.Errorf("systemd drop-in %s exceeds the size limit", path)
	}
	return data, nil
}

func validateDropInKeys(content string) error {
	section := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section != "[Unit]" && section != "[Service]" {
			return errors.New("cannot install with unknown systemd overrides")
		}
		check, known := dropInKeys[strings.TrimSpace(key)]
		if !known || !check(strings.TrimSpace(value)) {
			return errors.New("cannot install with unknown systemd overrides")
		}
	}
	return nil
}

func validStopFailureMode(value string) bool { return stopFailureModes[value] }

func validTimeSpan(value string) bool {
	if value == "infinity" {
		return true
	}
	number, unit := value, ""
	for index, character := range value {
		if character >= '0' && character <= '9' || character == '.' {
			continue
		}
		number, unit = value[:index], value[index:]
		break
	}
	return validTimeSpanNumber(number) && timeSpanUnits[unit]
}

func validTimeSpanNumber(number string) bool {
	if number == "" || strings.Count(number, ".") > 1 {
		return false
	}
	for _, character := range number {
		if (character < '0' || character > '9') && character != '.' {
			return false
		}
	}
	return true
}
