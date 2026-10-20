package remoteoperations

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestScannerCancelBeforeProcessStartIsSafe(t *testing.T) {
	command := exec.CommandContext(t.Context(), "/bin/cat")
	configureScannerProcess(command)
	if err := command.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
}
