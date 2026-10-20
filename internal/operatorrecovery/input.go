package operatorrecovery

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const maximumInputBytes = 2048

type secretInput struct {
	file     *os.File
	reader   *bufio.Reader
	terminal *unix.Termios
}

func openInput(fd int) (*secretInput, error) {
	var file *os.File
	if fd < 0 {
		opened, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return nil, errors.New("attach a TTY or supply --secret-fd explicitly")
		}
		file = opened
	} else {
		duplicate, err := unix.Dup(fd)
		if err != nil {
			return nil, errors.New("secret file descriptor is unavailable")
		}
		unix.CloseOnExec(duplicate)
		file = os.NewFile(uintptr(duplicate), "recovery-input")
	}
	input := &secretInput{file: file, reader: bufio.NewReaderSize(file, maximumInputBytes)}
	terminal, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	if err == nil {
		input.terminal = terminal
		hidden := *terminal
		hidden.Lflag &^= unix.ECHO | unix.ECHONL
		if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &hidden); err != nil {
			_ = file.Close()
			return nil, err
		}
	} else if fd < 0 {
		_ = file.Close()
		return nil, errors.New("recovery input is not a terminal")
	}
	return input, nil
}

func (input *secretInput) Close() error {
	var restoreErr error
	if input.terminal != nil {
		restoreErr = unix.IoctlSetTermios(int(input.file.Fd()), unix.TCSETS, input.terminal)
	}
	return errors.Join(restoreErr, input.file.Close())
}

func (input *secretInput) read() (string, error) {
	line, err := input.reader.ReadSlice('\n')
	if err != nil {
		return "", errors.New("recovery input requires a bounded newline-terminated value")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	clear(line)
	return value, nil
}

func collectUnlock(input *secretInput, output io.Writer, request *UnlockInput) error {
	for _, field := range []struct {
		prompt string
		value  *string
	}{
		{"Replacement password:", &request.Password},
		{"Repeat replacement password:", &request.Repeat},
		{"Confirmation:", &request.Confirmation},
	} {
		if _, err := io.WriteString(output, field.prompt+"\n"); err != nil {
			return err
		}
		value, err := input.read()
		if err != nil {
			return err
		}
		*field.value = value
	}
	return nil
}
