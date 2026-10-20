package diagnostics

import (
	"context"
	"time"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const defaultResultLimit = 600

// ResultQuery 选择一条诊断序列；结果只能来自 MTS。
type ResultQuery struct {
	JobID     string
	Metric    string
	Stat      string
	Direction string
	Protocol  string
	Role      string
	Hop       int
	Limit     int
}

// ResultSeries 是单一序列的数值点；空洞由时间戳缺口表达。
type ResultSeries struct {
	Metric string            `json:"metric"`
	Unit   string            `json:"unit"`
	Hop    int               `json:"hop,omitempty"`
	Stat   string            `json:"stat,omitempty"`
	Points []store.TimePoint `json:"points"`
}

// Results 读取一个作业在 MTS 中的数值序列；MTS 失败是显式存储错误。
func (service *Service) Results(ctx context.Context, actor Actor, query ResultQuery) (ResultSeries, error) {
	if query.Limit <= 0 || query.Limit > metricquery.MaximumPoints {
		query.Limit = defaultResultLimit
	}
	var job model.DiagnosticJob
	err := service.read(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		loaded, err := tx.job(query.JobID)
		if err != nil {
			return err
		}
		job = loaded
		return nil
	})
	if err != nil {
		return ResultSeries{}, err
	}
	if !metricMatchesKind(job.Kind, query.Metric) {
		return ResultSeries{}, ErrInvalid
	}
	from, to := resultWindow(job)
	points, err := service.metrics.ReadDiagnosticPoints(ctx, store.DiagnosticQuery{NodeID: job.SourceNodeID,
		JobID: job.ID, Metric: query.Metric, From: from, To: to, Hop: query.Hop, Stat: query.Stat,
		Direction: query.Direction, Protocol: query.Protocol, Role: query.Role, Limit: query.Limit})
	if err != nil {
		return ResultSeries{}, resultError(err)
	}
	return ResultSeries{Metric: query.Metric, Unit: diagnosticUnit(query.Metric), Hop: query.Hop,
		Stat: query.Stat, Points: points}, nil
}

func metricMatchesKind(kind, metric string) bool {
	if kind == model.DiagnosticKindMTR {
		return model.IsMTRDiagnosticMetric(metric)
	}
	return kind == model.DiagnosticKindIperf3 && model.IsIperfDiagnosticMetric(metric)
}

// resultWindow 覆盖作业生命周期；MTS 查询本身还有独立的范围上限。
func resultWindow(job model.DiagnosticJob) (time.Time, time.Time) {
	end := job.CreatedAt.Add(time.Hour)
	if job.CompletedAt != nil && job.CompletedAt.After(job.CreatedAt) {
		end = job.CompletedAt.Add(time.Minute)
	}
	return job.CreatedAt.Add(-time.Minute), end
}

func diagnosticUnit(metric string) string {
	switch metric {
	case model.DiagnosticMetricMTRLatencyMS, model.DiagnosticMetricIperfJitterMS:
		return "milliseconds"
	case model.DiagnosticMetricMTRLossPercent, model.DiagnosticMetricIperfLossPercent:
		return "percent"
	case model.DiagnosticMetricIperfBytesPerSecond:
		return "bytes_per_second"
	case model.DiagnosticMetricIperfBytes:
		return "bytes"
	default:
		return "count"
	}
}

func resultError(err error) error {
	switch {
	case err == nil:
		return nil
	case metricquery.ErrInvalid == err, metricquery.ErrCapacity == err:
		return ErrInvalid
	case metricquery.ErrArithmetic == err:
		return ErrUnavailable
	default:
		return ErrUnavailable
	}
}
