package store

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

type metricRetentionCutoff struct {
	measurement string
	cutoff      time.Time
}

func (s *MTSStore) metricRetentionPlan(ctx context.Context, defaultCutoff time.Time) ([]metricRetentionCutoff, error) {
	if s.catalog == nil {
		plans := make([]metricRetentionCutoff, 0)
		for _, name := range managedMeasurements() {
			plans = append(plans, metricRetentionCutoff{measurement: name, cutoff: defaultCutoff})
		}
		return plans, nil
	}
	catalog, err := s.catalog.Catalog(ctx, false)
	if err != nil {
		return nil, err
	}
	now := model.NowUTC()
	acceleratorCutoff := defaultCutoff
	plans := make([]metricRetentionCutoff, 0, len(catalog.Metrics)+1)
	for _, definition := range catalog.Metrics {
		cutoff := defaultCutoff
		if definition.RetentionOverride != nil {
			cutoff = now.AddDate(0, 0, -*definition.RetentionOverride)
		}
		if model.ValidAcceleratorMetric(definition.Key) && cutoff.Before(acceleratorCutoff) {
			acceleratorCutoff = cutoff
		}
		plans = append(plans, metricRetentionCutoff{measurement: definition.Key, cutoff: cutoff})
	}
	return append(plans, metricRetentionCutoff{measurement: networkProbeMeasurement, cutoff: defaultCutoff},
		metricRetentionCutoff{measurement: model.AgentReceiptMeasurement, cutoff: defaultCutoff}, metricRetentionCutoff{measurement: model.AcceleratorSampleMeasurement, cutoff: acceleratorCutoff}), nil
}

func (s *MTSStore) ApplyMetricRetention(ctx context.Context, job model.MetricRetentionJob) error {
	if !metriccatalog.ValidKey(job.Key) || job.RetentionDays < metriccatalog.MinimumRetentionDays || job.RetentionDays > metriccatalog.MaximumRetentionDays {
		return errors.New("invalid metric retention job")
	}
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	if err := s.engine.Flush(ctx); err != nil {
		return err
	}
	cutoff := job.StartedAt.AddDate(0, 0, -job.RetentionDays)
	if isTrafficDeltaMeasurement(job.Key) {
		protected, guardErr := s.protectedTrafficCutoff(ctx, cutoff, job.StartedAt)
		if guardErr != nil {
			return guardErr
		}
		cutoff = protected
	}
	if err := s.deleteMeasurementBefore(ctx, job.Key, cutoff); err != nil {
		return err
	}
	_, err = s.engine.CompactWithResult(ctx)
	return err
}
