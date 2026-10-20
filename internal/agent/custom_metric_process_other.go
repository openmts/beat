//go:build !linux

package agent

import (
	"errors"
	"os"
	"os/exec"
)

func configureCustomMetricCommand(_ *exec.Cmd) error {
	return errors.New("custom metric process isolation requires Linux")
}

func stopCustomMetricCommand(_ *exec.Cmd) error { return os.ErrProcessDone }
