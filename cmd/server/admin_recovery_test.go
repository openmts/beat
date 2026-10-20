package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRecoveryCommandDispatch(t *testing.T) {
	var output bytes.Buffer
	for _, args := range [][]string{nil, {"--version"}} {
		handled, err := handleRecoveryCommand(args, &output)
		if handled || err != nil {
			t.Fatal("normal flags intercepted")
		}
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	handled, err := handleRecoveryCommand([]string{"admin-recovery", "inspect", "--data-dir", directory}, &output)
	if !handled || err != nil || !strings.Contains(output.String(), "empty_instance") {
		t.Fatal("recovery inspection not dispatched")
	}
	handled, err = handleRecoveryCommand([]string{"admin-recovery"}, &output)
	if !handled || err == nil {
		t.Fatal("invalid recovery not rejected")
	}
}
