package acceleratorcollect

import (
	"context"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type Collector struct {
	mu       sync.RWMutex
	local    model.AcceleratorPolicy
	remote   *model.AcceleratorPolicy
	executor Executor
	latest   *model.AcceleratorReport
	wake     chan struct{}
	now      func() time.Time
	platform string
}

func New(config Config, executor Executor) (*Collector, error) {
	policy, err := config.Policy()
	if err != nil {
		return nil, err
	}
	if executor == nil {
		executor = commandExecutor{}
	}
	return &Collector{local: policy, executor: executor, wake: make(chan struct{}, 1), now: time.Now, platform: runtime.GOOS}, nil
}

func (c *Collector) ApplyPolicy(policy model.AcceleratorPolicy) error {
	if err := model.ValidateAcceleratorPolicy(model.AcceleratorPolicyWrite{Enabled: policy.Enabled, SampleIntervalSeconds: policy.SampleIntervalSeconds, MaximumDevices: policy.MaximumDevices, Metrics: policy.Metrics, ExpectedRevision: policy.Revision}); err != nil {
		return err
	}
	policy.Metrics = slices.Clone(policy.Metrics)
	c.mu.Lock()
	if c.remote != nil && samePolicy(*c.remote, policy) {
		c.mu.Unlock()
		return nil
	}
	c.remote = &policy
	c.latest = nil
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (c *Collector) policy() (model.AcceleratorPolicy, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.remote != nil && c.remote.Configured {
		return *c.remote, c.local.Enabled
	}
	return c.local, c.local.Enabled
}

func (c *Collector) Snapshot() *model.AcceleratorReport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return model.CloneAcceleratorReport(c.latest)
}

func (c *Collector) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.wake:
		case <-timer.C:
		}
		started := time.Now()
		report, err := c.Collect(ctx)
		if err != nil {
			return err
		}
		policy, _ := c.policy()
		c.mu.Lock()
		revision := c.local.Revision
		if c.remote != nil && c.remote.Configured {
			revision = c.remote.Revision
		}
		if report.PolicyRevision == revision {
			c.latest = report
		}
		c.mu.Unlock()
		timer.Reset(max(time.Duration(policy.SampleIntervalSeconds)*time.Second-time.Since(started), 0))
	}
}

func (c *Collector) Collect(ctx context.Context) (*model.AcceleratorReport, error) {
	policy, installation := c.policy()
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, ErrCollector
	}
	report := &model.AcceleratorReport{Version: 1, SampleID: id.String(), CadenceMS: int64(policy.SampleIntervalSeconds) * 1000, CollectorVersion: "1", InstallationEnabled: installation, PolicyRevision: policy.Revision, State: "disabled", Capabilities: []model.AcceleratorCapability{}, Devices: []model.AcceleratorDeviceReport{}}
	if !policy.Enabled {
		report.CapturedAt = c.now().UTC()
		return report, nil
	}
	if c.platform != "linux" {
		report.State, report.ErrorCode, report.CapturedAt = "unsupported", "platform_unsupported", c.now().UTC()
		return report, nil
	}
	bounded, cancel := context.WithTimeout(ctx, CollectionTimeout)
	defer cancel()
	for _, vendor := range []string{"nvidia", "amd"} {
		c.collectVendor(bounded, report, vendor)
	}
	report.CapturedAt = c.now().UTC()
	filterMetrics(report, policy.Metrics)
	if len(report.Devices) > policy.MaximumDevices {
		report.State, report.ErrorCode, report.Devices = "unavailable", "device_limit", []model.AcceleratorDeviceReport{}
		for index := range report.Capabilities {
			report.Capabilities[index].State = "unavailable"
			report.Capabilities[index].ErrorCode = "device_limit"
		}
	}
	sort.Slice(report.Devices, func(i, j int) bool { return report.Devices[i].Fingerprint < report.Devices[j].Fingerprint })
	if err := report.Validate(report.CapturedAt); err != nil {
		return nil, ErrInvalidOutput
	}
	return report, nil
}

func (c *Collector) collectVendor(ctx context.Context, report *model.AcceleratorReport, vendor string) {
	capability := model.AcceleratorCapability{Vendor: vendor, State: "ok", Metrics: slices.Clone(model.AcceleratorMetricKeys)}
	raw, err := c.executor.Run(ctx, vendor)
	var devices []model.AcceleratorDeviceReport
	if err == nil {
		if vendor == "nvidia" {
			devices, err = ParseNVIDIA(raw)
		} else {
			devices, err = ParseAMD(raw)
		}
	}
	if err != nil {
		capability.State, capability.ErrorCode = "unavailable", collectorError(err)
		if capability.ErrorCode == "collector_missing" {
			capability.State = "unsupported"
		}
	} else {
		for _, device := range devices {
			if len(device.Errors) > 0 {
				capability.State, capability.ErrorCode = "partial", "partial_metrics"
			}
		}
		report.Devices = append(report.Devices, devices...)
	}
	report.Capabilities = append(report.Capabilities, capability)
	report.State, report.ErrorCode = collectionState(report.Capabilities)
}

func samePolicy(left, right model.AcceleratorPolicy) bool {
	return left.Configured == right.Configured && left.Enabled == right.Enabled && left.Revision == right.Revision && left.SampleIntervalSeconds == right.SampleIntervalSeconds && left.MaximumDevices == right.MaximumDevices && slices.Equal(left.Metrics, right.Metrics)
}
