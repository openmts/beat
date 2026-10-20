package acceleratorcollect

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
)

type commandExecutor struct{}

var collectorArguments = map[string][]string{
	"nvidia": {"--query-gpu=pci.bus_id,name,driver_version,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit", "--format=csv,noheader,nounits"},
	"amd":    {"--showbus", "--showproductname", "--showdriverversion", "--showuse", "--showmeminfo", "vram", "--showtemp", "--showpower", "--showmaxpower", "--json"},
}

var collectorPaths = map[string]string{"nvidia": "/usr/bin/nvidia-smi", "amd": "/opt/rocm/bin/rocm-smi"}

type boundedOutput struct {
	mu       sync.Mutex
	bytes    []byte
	size     int
	overflow bool
	capture  bool
	shared   *boundedOutput
	cancel   context.CancelFunc
}

func (output *boundedOutput) Write(content []byte) (int, error) {
	shared := output.shared
	if shared == nil {
		shared = output
	}
	shared.mu.Lock()
	defer shared.mu.Unlock()
	shared.size += len(content)
	if shared.size > MaximumOutputBytes {
		shared.overflow = true
		if shared.cancel != nil {
			shared.cancel()
		}
		return 0, ErrOutputLimit
	}
	if output.capture {
		shared.bytes = append(shared.bytes, content...)
	}
	return len(content), nil
}

func (commandExecutor) Run(ctx context.Context, vendor string) ([]byte, error) {
	path, ok := collectorPaths[vendor]
	if !ok {
		return nil, ErrCollector
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return nil, ErrCollector
	}
	ctx, cancel := context.WithTimeout(ctx, CollectionTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path, collectorArguments[vendor]...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	output := &boundedOutput{capture: true, cancel: cancel}
	command.Stdout = output
	command.Stderr = &boundedOutput{shared: output}
	configureCommand(command)
	err = command.Run()
	cleanupErr := finishCommand(command)
	if output.overflow {
		return nil, ErrOutputLimit
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || cleanupErr != nil {
		return nil, ErrCollector
	}
	return output.bytes, nil
}

func collectorError(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "collector_missing"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "collector_timeout"
	case errors.Is(err, ErrOutputLimit):
		return "output_limit"
	case errors.Is(err, ErrInvalidOutput):
		return "parse_failed"
	default:
		return "collector_failed"
	}
}
