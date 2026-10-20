package agent

import (
	"time"

	"github.com/beat/backend/internal/model"
)

type AcceleratorSampler interface {
	Snapshot() *model.AcceleratorReport
}

func (runner *Runner) attachAccelerators(sample *ReportSample) {
	if runner.accelerators == nil {
		return
	}
	snapshot := runner.accelerators.Snapshot()
	if snapshot == nil || time.Since(snapshot.CapturedAt) > 2*model.MaximumAcceleratorInterval {
		return
	}
	sample.Accelerators = model.CloneAcceleratorReport(snapshot)
	if snapshot.CapturedAt.After(sample.CapturedAt) {
		sample.CapturedAt = snapshot.CapturedAt
	}
}

func (reporter *HTTPReporter) WithAcceleratorPolicyReceiver(receiver func(model.AcceleratorPolicy) error) *HTTPReporter {
	reporter.acceleratorPolicy = receiver
	return reporter
}
