// Command beat-cli 是 Beat 数据面的外部自动化客户端。
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/beat/backend/internal/beatcli"
)

func main() {
	os.Exit(commandExit(os.Args, os.Stdout, os.Stderr))
}

func commandExit(args []string, output, failures io.Writer) int {
	if len(args) == 0 {
		args = []string{"beat-cli"}
	}
	options := beatcli.Options{Output: output, Errors: failures, Stdin: os.Stdin, Getenv: os.Getenv}
	if err := beatcli.NewCommand(options).Run(context.Background(), args); err != nil {
		_, _ = fmt.Fprintf(failures, "beat-cli: %v\n", err)
		return 1
	}
	return 0
}
