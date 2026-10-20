package remoteoperations

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type CommandScannerConfig struct {
	Executable string
	Arguments  []string
	Timeout    time.Duration
}

type CommandScanner struct {
	executable string
	arguments  []string
	timeout    time.Duration
}

// NewCommandScanner 仅由固定启动配置创建，HTTP 和策略不接受程序路径或参数。
func NewCommandScanner(config CommandScannerConfig) (*CommandScanner, error) {
	if config.Executable == "" || !filepath.IsAbs(config.Executable) || len(config.Arguments) > 32 {
		return nil, ErrInvalid
	}
	for _, argument := range config.Arguments {
		if len(argument) > 1024 || strings.ContainsRune(argument, 0) {
			return nil, ErrInvalid
		}
	}
	if config.Timeout == 0 {
		config.Timeout = maximumScanDuration
	}
	if config.Timeout < time.Second || config.Timeout > maximumScanDuration {
		return nil, ErrInvalid
	}
	return &CommandScanner{executable: config.Executable, arguments: slices.Clone(config.Arguments), timeout: config.Timeout}, nil
}

func (scanner *CommandScanner) Scan(ctx context.Context, content io.Reader) error {
	ctx, cancel := context.WithTimeout(ctx, scanner.timeout)
	defer cancel()
	command := exec.CommandContext(ctx, scanner.executable, scanner.arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdin = content
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	configureScannerProcess(command)
	if err := command.Run(); err != nil {
		return errScanRejected
	}
	return nil
}
