//go:build !linux

package remoteoperations

import "os/exec"

func configureScannerProcess(_ *exec.Cmd) {}
