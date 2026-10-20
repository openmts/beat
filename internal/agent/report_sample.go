package agent

import (
	"errors"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type ReportSample struct {
	Accelerators  *model.AcceleratorReport
	SampleID      string
	CapturedAt    time.Time
	Metrics       model.NodeMetrics
	CustomMetrics map[string]float64
	CustomSamples map[string]model.CustomSampleMetadata
}

func newReportSample(metrics model.NodeMetrics, custom model.CollectedCustomSamples, now time.Time) (ReportSample, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return ReportSample{}, errors.New("create sample identity failed")
	}
	at := metrics.CapturedAt
	if at.IsZero() {
		at = now
	}
	for _, source := range custom.Samples {
		if source.CapturedAt.After(at) {
			at = source.CapturedAt
		}
	}
	return ReportSample{SampleID: id.String(), CapturedAt: at.UTC(), Metrics: metrics, CustomMetrics: maps.Clone(custom.Values), CustomSamples: maps.Clone(custom.Samples)}, nil
}
