package operatorrecovery

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func inputFile(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(privateTemp(t), "input")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestCommandParsingAndEmptyInspection(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"inspect", "--password=super-secret"}, {"inspect", "positional"}, {"unlock-owner", "--data-dir", "x"}, {"stage-restore", "--data-dir", "x"}, {"unlock-owner", "--data-dir", "x", "--secret-fd", "-2"}} {
		err := Run(args, io.Discard)
		if err == nil || strings.Contains(err.Error(), "super-secret") {
			t.Fatalf("unsafe parse failure: %v", err)
		}
	}
	directory := privateTemp(t)
	var output bytes.Buffer
	if err := Run([]string{"inspect", "--data-dir", directory}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "empty_instance") {
		t.Fatal("missing inspection")
	}
	if err := Run([]string{"inspect", "--data-dir", directory}, failedWriter{}); err == nil {
		t.Fatal("output error ignored")
	}
	if err := Run([]string{"inspect", "--data-dir", "/absent"}, io.Discard); err == nil {
		t.Fatal("preflight accepted")
	}
	if err := Run([]string{"stage-restore", "--data-dir", "/absent-parent/new", "--archive", "/absent"}, io.Discard); err == nil {
		t.Fatal("absent parent accepted")
	}
	directory = filepath.Join(privateTemp(t), "empty")
	if err := Run([]string{"stage-restore", "--data-dir", directory, "--archive", "/absent"}, io.Discard); err == nil {
		t.Fatal("missing archive accepted")
	}
}

func TestUnlockCommandWithExplicitDescriptor(t *testing.T) {
	config := fixture(t)
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	confirmation := session.Confirmation("owner")
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	file := inputFile(t, "unique recovery password\nunique recovery password\n"+confirmation+"\n")
	args := []string{"unlock-owner", "--data-dir", config.DataDir, "--username", "owner", "--secret-fd", strconv.Itoa(int(file.Fd()))}
	var output bytes.Buffer
	if err := Run(args, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "owner_unlocked") || strings.Contains(output.String(), "unique recovery password") {
		t.Fatal("missing success or leaked password")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("caller FD closed")
	}
}

func TestUnlockCommandFailures(t *testing.T) {
	config := fixture(t)
	session, err := Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	options := commandOptions{operation: "unlock-owner", username: "owner", fd: 999999}
	if err := runUnlock(session, options, io.Discard); err == nil {
		t.Fatal("invalid FD accepted")
	}
	if err := runUnlock(session, options, failedWriter{}); err == nil {
		t.Fatal("output failure ignored")
	}
	options.username = "missing"
	if err := runUnlock(session, options, io.Discard); err == nil {
		t.Fatal("unknown owner accepted")
	}
	options.username = "owner"
	for _, content := range []string{"incomplete", "valid password\nvalid password\nincorrect\n"} {
		file := inputFile(t, content)
		options.fd = int(file.Fd())
		if err := runUnlock(session, options, io.Discard); err == nil {
			t.Fatal("bad input accepted")
		}
	}
}

func TestSecretReaderBoundsAndFailures(t *testing.T) {
	for _, content := range []string{"value\r\n", "missing newline", strings.Repeat("x", maximumInputBytes) + "\n"} {
		file := inputFile(t, content)
		input, err := openInput(int(file.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		value, err := input.read()
		if strings.HasPrefix(content, "value") && (err != nil || value != "value") {
			t.Fatal("CRLF failed")
		}
		if !strings.HasPrefix(content, "value") && err == nil {
			t.Fatal("unbounded input accepted")
		}
		if err := input.Close(); err != nil {
			t.Fatal(err)
		}
	}
	file := inputFile(t, "unused\n")
	input, err := openInput(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	if err := collectUnlock(input, failedWriter{}, &UnlockInput{}); err == nil {
		t.Fatal("prompt failure ignored")
	}
	if _, err := openInput(999999); err == nil {
		t.Fatal("missing FD accepted")
	}
	// 测试进程通常无控制TTY；存在TTY时仍正确清理而不读取。
	tty, err := openInput(-1)
	if err == nil {
		_ = tty.Close()
	}
}

func TestTerminalEchoIsDisabledAndRestored(t *testing.T) {
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(masterFD), "pty-master")
	defer func() { _ = master.Close() }()
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = slave.Close() }()
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	input, err := openInput(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || hidden.Lflag&unix.ECHO != 0 {
		t.Fatal("TTY echo enabled")
	}
	if _, err := master.WriteString("terminal secret\n"); err != nil {
		t.Fatal(err)
	}
	value, err := input.read()
	if err != nil || value != "terminal secret" {
		t.Fatal("TTY read failed")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || before.Lflag != after.Lflag {
		t.Fatal("TTY settings not restored")
	}
}
