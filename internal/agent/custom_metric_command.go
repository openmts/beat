package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	customMetricCommandTimeout = 10 * time.Second
	customMetricDrainTimeout   = 250 * time.Millisecond
	maxCustomMetricOutput      = 64 << 10
)

var errCustomMetricOutput = errors.New("custom metric output exceeds 64 KiB")

var errCustomMetricCommand = errors.New("custom metric command failed")

type customMetricOutput struct {
	mu       sync.Mutex
	stdout   []byte
	bytes    int
	exceeded bool
	cancel   context.CancelFunc
}

type customMetricWriter struct {
	output *customMetricOutput
	stdout bool
}

func (writer customMetricWriter) Write(value []byte) (int, error) {
	output := writer.output
	output.mu.Lock()
	if output.exceeded || len(value) > maxCustomMetricOutput-output.bytes {
		output.exceeded = true
		output.mu.Unlock()
		output.cancel()
		return 0, errCustomMetricOutput
	}
	output.bytes += len(value)
	if writer.stdout {
		output.stdout = append(output.stdout, value...)
	}
	output.mu.Unlock()
	return len(value), nil
}

func executeCustomMetric(ctx context.Context, metric registeredMetric) (float64, error) {
	value, _, err := runCustomMetricCommand(ctx, metric.ShellCommand)
	return value, err
}

// runCustomMetricCommand 执行一次采集命令，同时返回未解析的 stdout 前缀，
// 供「试运行」把原始输出当作诊断证据回传；返回值本身仍走同一套解析规则。
func runCustomMetricCommand(ctx context.Context, shellCommand string) (float64, string, error) {
	if strings.TrimSpace(shellCommand) == "" {
		return 0, "", errCustomMetricCommand
	}
	commandCtx, cancel := context.WithTimeout(ctx, customMetricCommandTimeout)
	defer cancel()
	output := &customMetricOutput{cancel: cancel}
	command := exec.CommandContext(commandCtx, "sh", "-c", shellCommand)
	command.Stdout = customMetricWriter{output: output, stdout: true}
	command.Stderr = customMetricWriter{output: output}
	command.WaitDelay = customMetricDrainTimeout
	if err := configureCustomMetricCommand(command); err != nil {
		return 0, "", err
	}
	err := command.Run()
	stopErr := stopCustomMetricCommand(command)
	if stopErr != nil && !errors.Is(stopErr, os.ErrProcessDone) {
		err = errors.Join(err, stopErr)
	}
	output.mu.Lock()
	defer output.mu.Unlock()
	raw := string(output.stdout)
	if output.exceeded {
		return 0, raw, errCustomMetricOutput
	}
	if commandCtx.Err() != nil {
		return 0, raw, commandCtx.Err()
	}
	if err != nil {
		return 0, raw, errCustomMetricCommand
	}
	value, parseErr := parseMetricValue(raw)
	if parseErr != nil {
		return 0, raw, parseErr
	}
	return value, raw, nil
}
