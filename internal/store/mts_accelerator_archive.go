package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/openmts/mts"
)

// ValidateAcceleratorArchiveMemory 对临时MTS中的同设备同采样时刻执行跨measurement校验。
// 独立保留策略可能已删除其中一项，缺失项不能被当作零。
func (s *MTSStore) ValidateAcceleratorArchiveMemory(ctx context.Context) error {
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	_, err = s.exportMeasurement(ctx, time.Unix(0, math.MaxInt64), measurementSchema{name: "gpu_memory_used_bytes", fields: []string{"value"}}, func(row mts.Row) error {
		return s.validateAcceleratorMemoryPair(ctx, row)
	})
	return err
}

func (s *MTSStore) validateAcceleratorMemoryPair(ctx context.Context, used mts.Row) error {
	if used.Timestamp == math.MaxInt64 {
		return errors.New("invalid accelerator archive timestamp")
	}
	query, err := mts.NewQuery().From("beat", "", "gpu_memory_total_bytes").Select("value").
		TimeRange(used.Timestamp, used.Timestamp+1).Precision(mts.PrecisionNanosecond).
		Where(mts.TagEq("node", used.Tags["node"]), mts.TagEq("device", used.Tags["device"]), mts.TagEq("vendor", used.Tags["vendor"])).OrderByTimeAsc().Limit(1).Build()
	if err != nil {
		return err
	}
	rows, err := s.engine.QueryRows(ctx, query)
	if err != nil {
		return err
	}
	if len(rows) > 0 && rows[0].Timestamp == used.Timestamp && used.Fields["value"].Float64 > rows[0].Fields["value"].Float64 {
		return errors.New("accelerator archive memory usage exceeds capacity")
	}
	return ctx.Err()
}
