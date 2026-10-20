package agent

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/model"
)

type customSourceSampler interface {
	SampleProvenance(context.Context) model.CollectedCustomSamples
}

func (r *Runner) collectCustomSamples(ctx context.Context) (model.CollectedCustomSamples, error) {
	if r.customSampler == nil {
		return model.CollectedCustomSamples{}, nil
	}
	if sampler, ok := r.customSampler.(customSourceSampler); ok {
		return sampler.SampleProvenance(ctx), nil
	}
	if values := r.customSampler.Sample(ctx); len(values) > 0 {
		return model.CollectedCustomSamples{}, errors.New("custom sample provenance is required")
	}
	return model.CollectedCustomSamples{}, nil
}

func (collector *CustomMetricCollector) SampleProvenance(ctx context.Context) model.CollectedCustomSamples {
	samples := collector.drainSamples(ctx)
	result := model.CollectedCustomSamples{Values: make(map[string]float64, len(samples)), Samples: make(map[string]model.CustomSampleMetadata, len(samples))}
	for name, sample := range samples {
		result.Values[name] = sample.value
		result.Samples[name] = sample.metadata
	}
	return result
}
