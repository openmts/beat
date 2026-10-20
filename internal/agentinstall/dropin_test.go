package agentinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const benignDropInBody = "[Unit]\n# Fedora shorter shutdown timer\n[Service]\nTimeoutStopFailureMode=abort\nTimeoutStopSec=90s\n"

func newDropInInstaller(t *testing.T) (*Installer, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "systemd")
	installer := NewInstaller()
	installer.dropInRoots = []string{root}
	return installer, filepath.Join(root, "service.d")
}

func writeDropIn(t *testing.T, directory, name, body string, mode os.FileMode) string {
	t.Helper()
	mustMkdir(t, directory)
	path := filepath.Join(directory, name)
	mustWrite(t, path, []byte(body), mode)
	return path
}

func unitStatus(path string, dropIns ...string) string {
	return "LoadState=loaded\nFragmentPath=" + path + "\nDropInPaths=" + strings.Join(dropIns, " ") + "\n"
}

func TestAdmitSystemdUnitAcceptsBenignVendorDropIn(t *testing.T) {
	installer, directory := newDropInInstaller(t)
	first := writeDropIn(t, directory, "10-timeout-abort.conf", benignDropInBody, 0o644)
	second := writeDropIn(t, directory, "20-timeout-stop.conf", "[Service]\nTimeoutAbortSec=infinity\n", 0o644)
	output := unitStatus(installer.paths.unit, first, second)
	if err := installer.admitSystemdUnit(output, installer.paths.unit); err != nil {
		t.Fatal(err)
	}
}

func TestAdmitSystemdUnitRejectsUnsafeDropInPaths(t *testing.T) {
	installer, directory := newDropInInstaller(t)
	known := writeDropIn(t, directory, "10-timeout-abort.conf", benignDropInBody, 0o644)
	cases := map[string]string{
		"outside vendor roots":  "/etc/systemd/system/" + ServiceName + ".d/override.conf",
		"prefix lookalike":      filepath.Dir(filepath.Dir(directory)) + "-evil/service.d/override.conf",
		"traversal":             filepath.Join(filepath.Dir(directory), "..", "service.d", "override.conf"),
		"relative":              "service.d/override.conf",
		"not a conf file":       strings.TrimSuffix(known, ".conf"),
		"not a drop-in dir":     filepath.Join(filepath.Dir(directory), "10-timeout-abort.conf"),
		"missing vendor file":   filepath.Join(directory, "missing.conf"),
		"unsafe vendor file":    writeDropIn(t, directory, "30-writable.conf", benignDropInBody, 0o666),
		"symlinked vendor file": symlinkDropIn(t, directory),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			output := unitStatus(installer.paths.unit, known, path)
			if err := installer.admitSystemdUnit(output, installer.paths.unit); err == nil {
				t.Fatalf("accepted drop-in %q", path)
			}
		})
	}
}

func symlinkDropIn(t *testing.T, directory string) string {
	t.Helper()
	mustMkdir(t, directory)
	target := filepath.Join(directory, "40-target.conf")
	mustWrite(t, target, []byte(benignDropInBody), 0o644)
	link := filepath.Join(directory, "40-link.conf")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func TestAdmitSystemdUnitRejectsUnsafeDropInKeys(t *testing.T) {
	cases := map[string]string{
		"exec":            "[Service]\nExecStart=/bin/sh -c id\n",
		"user":            "[Service]\nUser=root\n",
		"group":           "[Service]\nGroup=root\n",
		"environment":     "[Service]\nEnvironment=BEAT_X=1\n",
		"restart":         "[Service]\nRestart=always\n",
		"unknown key":     "[Service]\nPrivateTmp=yes\n",
		"invalid mode":    "[Service]\nTimeoutStopFailureMode=explode\n",
		"invalid span":    "[Service]\nTimeoutStopSec=abc\n",
		"invalid unit":    "[Service]\nTimeoutStopSec=5x\n",
		"shell value":     "[Service]\nTimeoutStopSec=$(reboot)\n",
		"missing section": "TimeoutStopSec=5s\n",
		"irrelevant unit": "[Install]\nTimeoutStopSec=5s\n",
		"malformed line":  "[Service]\nTimeoutStopSec\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			installer, directory := newDropInInstaller(t)
			path := writeDropIn(t, directory, "10-override.conf", body, 0o644)
			output := unitStatus(installer.paths.unit, path)
			if err := installer.admitSystemdUnit(output, installer.paths.unit); err == nil {
				t.Fatalf("accepted drop-in body %q", body)
			}
		})
	}
}

func TestCheckVendorDropInPath(t *testing.T) {
	roots := []string{"/usr/lib/systemd/system"}
	accepted := "/usr/lib/systemd/system/service.d/10-timeout-abort.conf"
	if err := checkVendorDropInPath(accepted, roots); err != nil {
		t.Fatal(err)
	}
	rejected := []string{"/usr/lib/systemd/service.d/10-timeout-abort.conf",
		"/usr/lib/systemd/system/service/10-timeout-abort.conf",
		"/etc/systemd/system/beat-agent.service.d/10-config.conf"}
	for _, path := range rejected {
		if err := checkVendorDropInPath(path, roots); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}

func TestValidTimeSpan(t *testing.T) {
	for _, value := range []string{"infinity", "0", "5s", "1.5s", "1min", "2m", "1h", "3us"} {
		if !validTimeSpan(value) {
			t.Fatalf("rejected %q", value)
		}
	}
	for _, value := range []string{"", " ", "5x", ";", "1..5s", "5 s", "-1s", "infinity "} {
		if validTimeSpan(value) {
			t.Fatalf("accepted %q", value)
		}
	}
}
