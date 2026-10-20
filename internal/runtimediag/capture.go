package runtimediag

import (
	"context"
	"errors"
	"io"
	"runtime/pprof"
	"runtime/trace"
	"time"
)

// ProfileCapture 是生产捕获实现：定时类型使用 CPU/trace 采样，瞬时类型固定 debug level 0。
// 它不修改全局 mutex/block 采样率，因此 mutex/block 只反映部署当前的运行时设置。
func ProfileCapture(ctx context.Context, kind string, duration time.Duration, writer io.Writer) error {
	switch kind {
	case KindCPU:
		return captureCPU(ctx, duration, writer)
	case KindTrace:
		return captureTrace(ctx, duration, writer)
	default:
		return captureInstant(kind, writer)
	}
}

func captureCPU(ctx context.Context, duration time.Duration, writer io.Writer) error {
	if err := pprof.StartCPUProfile(writer); err != nil {
		return err
	}
	waitForCapture(ctx, duration)
	pprof.StopCPUProfile()
	return ctx.Err()
}

func captureTrace(ctx context.Context, duration time.Duration, writer io.Writer) error {
	if err := trace.Start(writer); err != nil {
		return err
	}
	waitForCapture(ctx, duration)
	trace.Stop()
	return ctx.Err()
}

func captureInstant(kind string, writer io.Writer) error {
	profile := pprof.Lookup(kind)
	if profile == nil {
		return errors.New("runtime profile is unavailable")
	}
	return profile.WriteTo(writer, 0)
}

// waitForCapture 等待固定时长或取消；取消后调用方仍需停止采样再返回。
func waitForCapture(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
