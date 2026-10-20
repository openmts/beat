package operatorrecovery

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRootlessRecovery(t *testing.T) {
	if directory := os.Getenv("BEAT_TEST_ROOTLESS_RECOVERY"); directory != "" {
		session, err := Open(t.Context(), Config{DataDir: directory})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = session.Close() }()
		input := UnlockInput{Username: "owner", Password: "rootless recovery password", Repeat: "rootless recovery password", Confirmation: session.Confirmation("owner")}
		if err := session.Unlock(t.Context(), input); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Geteuid() != 0 {
		config := fixture(t)
		session, err := Open(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		_ = session.Close()
		return
	}
	config := fixture(t)
	root, err := os.MkdirTemp("", "beat-rootless-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	const uid = 65534
	if err := os.Chown(root, uid, uid); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(data, uid, uid); err != nil {
		t.Fatal(err)
	}
	copyOwnedTestFile(t, config.DatabasePath, filepath.Join(data, "beat.db"))
	copyOwnedTestFile(t, filepath.Join(config.DataDir, "admin-data.key"), filepath.Join(data, "admin-data.key"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "recovery-test")
	copyOwnedTestFile(t, executable, binary)
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestRootlessRecovery$", "-test.v")
	command.Env = append(os.Environ(), "BEAT_TEST_ROOTLESS_RECOVERY="+data)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("rootless child: %v\n%s", err, output)
	}
}

func copyOwnedTestFile(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(destination, 65534, 65534); err != nil {
		t.Fatal(err)
	}
}
