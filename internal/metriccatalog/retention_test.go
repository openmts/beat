package metriccatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type retentionFake struct {
	job                                         *model.MetricRetentionJob
	claimErr, applyErr, completeErr, recoverErr error
	result                                      *model.MetricRetentionCompletion
}

func (fake *retentionFake) RecoverMetricRetention(context.Context) error { return fake.recoverErr }

func (fake *retentionFake) ClaimMetricRetention(context.Context) (*model.MetricRetentionJob, error) {
	return fake.job, fake.claimErr
}

func (fake *retentionFake) CompleteMetricRetention(ctx context.Context, result model.MetricRetentionCompletion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fake.result = &result
	return fake.completeErr
}

func (fake *retentionFake) ApplyMetricRetention(context.Context, model.MetricRetentionJob) error {
	return fake.applyErr
}

func TestMetricRetentionRunnerPersistsFailuresWithoutCanceledRequestContext(t *testing.T) {
	for _, failure := range []error{nil, errors.New("storage failure")} {
		fake := &retentionFake{job: &model.MetricRetentionJob{Key: "cpu", RetentionDays: 1, Revision: 1}, applyErr: failure}
		runner := NewRetentionRunner(fake, fake)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := runner.RunOnce(ctx)
		if !errors.Is(err, failure) || fake.result == nil || (fake.result.Error != "") != (failure != nil) {
			t.Fatalf("completion=%v/%v", fake.result, err)
		}
	}
}

func TestMetricRetentionRunnerHandlesEmptyQueueAndStorageErrors(t *testing.T) {
	failure := errors.New("queue unavailable")
	fake := &retentionFake{claimErr: failure}
	runner := NewRetentionRunner(fake, fake)
	if !errors.Is(runner.RunOnce(t.Context()), failure) {
		t.Fatal("queue error ignored")
	}
	fake.claimErr = nil
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	observed := make(chan error, 2)
	runner.SetObserver(func(err error) { observed <- err; cancel() })
	done := make(chan struct{})
	go func() { runner.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention loop did not stop")
	}
	if runner.Interval() != RetentionInterval {
		t.Fatal("retention interval drifted")
	}
}
