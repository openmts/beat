package store

import (
	"context"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

type MetricReportSample struct {
	Accelerators    *model.AcceleratorReport
	NodeID          string
	Metrics         *model.NodeMetrics
	Custom          []model.MetricValue
	EnabledBuiltins map[string]bool
	Timestamp       time.Time
}

func (s *MTSStore) WriteMetricReport(ctx context.Context, sample MetricReportSample) error {
	if sample.Metrics != nil {
		if err := sample.Metrics.Validate(); err != nil {
			return err
		}
	}
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.ensureOpen(); err != nil {
		return err
	}
	lock := s.nodeLock(sample.NodeID)
	lock.Lock()
	defer lock.Unlock()
	values, err := s.metricReportValues(ctx, sample)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return s.writeMetricValuesLocked(ctx, sample.NodeID, values, sample.Timestamp)
}

func (s *MTSStore) metricReportValues(ctx context.Context, sample MetricReportSample) ([]model.MetricValue, error) {
	values := make([]model.MetricValue, 0, len(sample.Custom)+len(knownMetrics))
	if sample.Metrics != nil {
		for _, metric := range sample.Metrics.TimeSeries() {
			if sample.allows(metric.Name) {
				values = append(values, metric)
			}
		}
		derived, err := s.metricReportTraffic(ctx, sample)
		if err != nil {
			return nil, err
		}
		values = append(values, derived...)
	}
	return append(values, sample.Custom...), nil
}

func (sample MetricReportSample) allows(key string) bool {
	return sample.EnabledBuiltins == nil || sample.EnabledBuiltins[key]
}

func (s *MTSStore) metricReportTraffic(ctx context.Context, sample MetricReportSample) ([]model.MetricValue, error) {
	values := make([]model.MetricValue, 0, 2)
	for _, counter := range []struct {
		source, target string
		value          float64
	}{
		{"net_recv_total", trafficReceivedDelta, sample.Metrics.NetRecvTotal},
		{"net_sent_total", trafficSentDelta, sample.Metrics.NetSentTotal},
	} {
		if !sample.allows(counter.target) {
			continue
		}
		if !sample.allows(counter.source) {
			return nil, errors.New("traffic deltas require their persisted source counter")
		}
		value, err := s.counterDelta(ctx, sample.NodeID, counter.source, counter.value, sample.Timestamp)
		if err != nil {
			return nil, err
		}
		values = append(values, model.MetricValue{Name: counter.target, Value: value})
	}
	return values, nil
}
