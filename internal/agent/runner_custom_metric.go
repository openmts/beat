package agent

import (
	"context"
	"fmt"
)

type backgroundCustomMetricSampler interface{ Run(context.Context) error }

func (runner *Runner) startCustomSampler(ctx context.Context) (<-chan error, func()) {
	sampler, ok := runner.customSampler.(backgroundCustomMetricSampler)
	if !ok {
		return nil, func() {}
	}
	customCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); result <- sampler.Run(customCtx) }()
	return result, func() { cancel(); <-done }
}

func (runner *Runner) reportCustomSamplerError(ctx context.Context, err error) {
	if err != nil && ctx.Err() == nil {
		runner.onError(fmt.Errorf("custom metric collector: %w", err))
	}
}
