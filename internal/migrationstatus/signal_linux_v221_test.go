//go:build linux

package migrationstatus

import (
	"os/signal"
	"syscall"
)

func signalIgnoreSIGXFSZ() { signal.Ignore(syscall.SIGXFSZ) }
