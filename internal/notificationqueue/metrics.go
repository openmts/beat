package notificationqueue

import (
	"time"

	"github.com/beat/backend/internal/model"
)

var metricProviders = [...]string{"webhook", "telegram", "email", "bark", "serverchan_turbo", "serverchan_3", "custom_http", "unknown"}

var dispatchBounds = [...]time.Duration{10 * time.Millisecond, 100 * time.Millisecond, time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

type ProviderDispatchMetrics struct {
	Provider             string
	Succeeded, Retryable uint64
	Retried              uint64
	Permanent, Dead      uint64
	DurationCount        uint64
	DurationSeconds      float64
	DurationBucketCount  [len(dispatchBounds)]uint64
}

type attemptObservation struct {
	lease    deliveryLease
	result   model.NotificationSendResult
	duration time.Duration
}

func (service *Service) recordAttempt(input attemptObservation) {
	index := len(metricProviders) - 1
	for position, provider := range metricProviders {
		if provider == input.lease.ChannelType {
			index = position
			break
		}
	}
	service.metricsMu.Lock()
	defer service.metricsMu.Unlock()
	metrics := &service.dispatchMetrics[index]
	metrics.DurationCount++
	metrics.DurationSeconds += max(0, input.duration.Seconds())
	for bucket, bound := range dispatchBounds {
		if input.duration <= bound {
			metrics.DurationBucketCount[bucket]++
		}
	}
	if input.result.Success {
		metrics.Succeeded++
	} else if input.result.Retryable {
		metrics.Retryable++
		if input.lease.Attempt >= 6 {
			metrics.Dead++
		} else {
			metrics.Retried++
		}
	} else {
		metrics.Permanent++
		metrics.Dead++
	}
}

func (service *Service) DispatchMetrics() []ProviderDispatchMetrics {
	service.metricsMu.Lock()
	defer service.metricsMu.Unlock()
	result := make([]ProviderDispatchMetrics, len(metricProviders))
	copy(result, service.dispatchMetrics[:])
	for index, provider := range metricProviders {
		result[index].Provider = provider
	}
	return result
}

func DispatchDurationBounds() []time.Duration {
	return append([]time.Duration(nil), dispatchBounds[:]...)
}
