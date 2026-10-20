package acceleratorcollect

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/beat/backend/internal/model"
)

var ErrCollector = errors.New("accelerator collector unavailable")

var ErrOutputLimit = errors.New("accelerator collector output limit")

var ErrInvalidOutput = errors.New("invalid accelerator collector output")

const MaximumOutputBytes = 256 << 10

const CollectionTimeout = 5 * time.Second

type Config struct {
	Enabled               bool     `json:"enabled"`
	SampleIntervalSeconds int      `json:"sample_interval_seconds"`
	MaximumDevices        int      `json:"maximum_devices"`
	Metrics               []string `json:"metrics"`
}

func (config Config) Policy() (model.AcceleratorPolicy, error) {
	policy := model.DefaultAcceleratorPolicy()
	if config.SampleIntervalSeconds != 0 {
		policy.SampleIntervalSeconds = config.SampleIntervalSeconds
	}
	if config.MaximumDevices != 0 {
		policy.MaximumDevices = config.MaximumDevices
	}
	if config.Metrics != nil {
		policy.Metrics = slices.Clone(config.Metrics)
	}
	policy.Enabled = config.Enabled
	err := model.ValidateAcceleratorPolicy(model.AcceleratorPolicyWrite{Enabled: policy.Enabled, SampleIntervalSeconds: policy.SampleIntervalSeconds, MaximumDevices: policy.MaximumDevices, Metrics: policy.Metrics})
	return policy, err
}

type Executor interface {
	Run(context.Context, string) ([]byte, error)
}
