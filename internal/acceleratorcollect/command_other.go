//go:build !linux

package acceleratorcollect

import (
	"os/exec"
	"time"
)

func configureCommand(command *exec.Cmd) { command.WaitDelay = 100 * time.Millisecond }

func finishCommand(*exec.Cmd) error { return nil }
