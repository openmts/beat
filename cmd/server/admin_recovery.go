package main

import (
	"io"

	"github.com/beat/backend/internal/operatorrecovery"
)

func handleRecoveryCommand(args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "admin-recovery" {
		return false, nil
	}
	return true, operatorrecovery.Run(args[1:], output)
}
