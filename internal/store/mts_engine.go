package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openmts/mts"
)

var ErrMTSRecoveryRequired = errors.New("MTS recovery is required before further writes or acknowledgements")

// trackedMTSEngine 统一跟踪底层关闭；上游关闭后的查询/Health仍可能返回空或健康。
type trackedMTSEngine struct {
	*mts.Engine
	closed   atomic.Bool
	poisoned atomic.Bool
	writeMu  sync.Mutex
}

// 上游无法区分写入失败阶段，尤其短写后不能继续向坏 WAL 尾部追加并 ACK。
func (engine *trackedMTSEngine) Write(ctx context.Context, points []mts.Point, options mts.WriteOptions) error {
	unlock, err := engine.lockWrites(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := engine.writable(); err != nil {
		return err
	}
	if err := engine.Engine.Write(ctx, points, options); err != nil {
		engine.poisoned.Store(true)
		return errors.Join(ErrMTSRecoveryRequired, err)
	}
	return nil
}

func (engine *trackedMTSEngine) confirmDurability(ctx context.Context) error {
	unlock, err := engine.lockWrites(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := engine.writable(); err != nil {
		return err
	}
	if err := engine.Flush(ctx); err != nil {
		engine.poisoned.Store(true)
		return errors.Join(ErrMTSRecoveryRequired, err)
	}
	return nil
}

func (engine *trackedMTSEngine) writable() error {
	if engine.closed.Load() {
		return ErrMTSStoreClosed
	}
	if engine.poisoned.Load() {
		return ErrMTSRecoveryRequired
	}
	return nil
}

func (engine *trackedMTSEngine) lockWrites(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if engine.writeMu.TryLock() {
		return engine.writeMu.Unlock, nil
	}
	ticker := time.NewTicker(metricOperationPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if engine.writeMu.TryLock() {
				return engine.writeMu.Unlock, nil
			}
		}
	}
}

func (engine *trackedMTSEngine) Close(ctx context.Context) error {
	if engine.closed.Swap(true) {
		return nil
	}
	return engine.Engine.Close(ctx)
}
