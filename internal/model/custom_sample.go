package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const MaximumCustomSampleAge = time.Minute

const MaximumCustomSourcesBytes = 24 << 10

const MinimumCustomCadence = 5 * time.Second

type CustomSampleMetadata struct {
	SampleID          string    `json:"sample_id"`
	CapturedAt        time.Time `json:"captured_at"`
	CadenceMS         int64     `json:"cadence_ms"`
	DefinitionVersion string    `json:"definition_version"`
}

type CollectedCustomSamples struct {
	Values  map[string]float64
	Samples map[string]CustomSampleMetadata
}

func CustomMetricDefinitionVersion(metric CustomMetric) string {
	hash := sha256.New()
	for _, field := range []string{metric.ID, metric.Name, metric.ShellCommand, strconv.Itoa(metric.IntervalSeconds), metric.UpdatedAt.UTC().Format(time.RFC3339Nano)} {
		_, _ = fmt.Fprintf(hash, "%d:%s", len(field), field)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func ValidateCustomSamples(values map[string]float64, samples map[string]CustomSampleMetadata, reportAt time.Time) error {
	if len(values) != len(samples) {
		return ErrAgentReportInvalid
	}
	if err := ValidateCustomSampleMetadata(samples, reportAt); err != nil {
		return err
	}
	for name := range values {
		if _, ok := samples[name]; !ok {
			return ErrAgentReportInvalid
		}
	}
	return nil
}

func ValidateCustomSampleMetadata(samples map[string]CustomSampleMetadata, reportAt time.Time) error {
	if len(samples) > MaximumAgentCustomMetrics {
		return ErrAgentReportInvalid
	}
	for name, sample := range samples {
		if !validAgentCustomName(name) || !validCustomSampleIdentity(sample) {
			return ErrAgentReportInvalid
		}
		if sample.CapturedAt.IsZero() || sample.CapturedAt.After(reportAt) || sample.CapturedAt.Before(reportAt.Add(-MaximumCustomSampleAge)) {
			return ErrAgentReportInvalid
		}
	}
	encoded, err := EncodeCustomSampleMetadata(samples)
	if err != nil || len(encoded) > MaximumCustomSourcesBytes {
		return ErrAgentReportInvalid
	}
	return nil
}

func validCustomSampleIdentity(sample CustomSampleMetadata) bool {
	id, err := uuid.Parse(sample.SampleID)
	if err != nil || id == uuid.Nil || id.String() != sample.SampleID {
		return false
	}
	if sample.CadenceMS < MinimumCustomCadence.Milliseconds() || sample.CadenceMS > MaximumAgentReportInterval.Milliseconds() {
		return false
	}
	digest, err := hex.DecodeString(sample.DefinitionVersion)
	return err == nil && len(digest) == sha256.Size && hex.EncodeToString(digest) == sample.DefinitionVersion
}

func EncodeCustomSampleMetadata(samples map[string]CustomSampleMetadata) (string, error) {
	normalized := maps.Clone(samples)
	if normalized == nil {
		normalized = make(map[string]CustomSampleMetadata)
	}
	for name, sample := range normalized {
		sample.CapturedAt = sample.CapturedAt.UTC()
		normalized[name] = sample
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", ErrAgentReportInvalid
	}
	return string(encoded), nil
}

func DecodeCustomSampleMetadata(encoded string, reportAt time.Time) (map[string]CustomSampleMetadata, error) {
	if len(encoded) > MaximumCustomSourcesBytes {
		return nil, ErrAgentReportInvalid
	}
	var samples map[string]CustomSampleMetadata
	if err := json.Unmarshal([]byte(encoded), &samples); err != nil || samples == nil {
		return nil, ErrAgentReportInvalid
	}
	if err := ValidateCustomSampleMetadata(samples, reportAt); err != nil {
		return nil, err
	}
	normalized, err := EncodeCustomSampleMetadata(samples)
	if err != nil || normalized != encoded {
		return nil, ErrAgentReportInvalid
	}
	return samples, nil
}
