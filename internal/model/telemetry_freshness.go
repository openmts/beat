package model

import "time"

const (
	TelemetryFresh          = "fresh"
	TelemetryDelayed        = "delayed"
	TelemetryStale          = "stale"
	TelemetryUnavailable    = "unavailable"
	minimumFreshTelemetry   = 30 * time.Second
	minimumDelayedTelemetry = 90 * time.Second
	freshTelemetryPeriods   = 2
	delayedTelemetryPeriods = 6
)

type TelemetryFreshness struct {
	TelemetryAt         *time.Time `json:"telemetry_at"`
	TelemetryAgeSeconds *int64     `json:"telemetry_age_seconds"`
	TelemetryState      string     `json:"telemetry_state"`
}

func NewTelemetryFreshness(now time.Time, capturedAt *time.Time, interval time.Duration) TelemetryFreshness {
	result := TelemetryFreshness{TelemetryState: TelemetryUnavailable}
	if capturedAt == nil || capturedAt.IsZero() {
		return result
	}
	interval = min(max(interval, MinimumAgentReportInterval), MaximumAgentReportInterval)
	at := capturedAt.UTC()
	age := max(now.Sub(at), 0)
	seconds := int64(age / time.Second)
	result.TelemetryAt, result.TelemetryAgeSeconds = &at, &seconds
	switch {
	case age <= max(interval*freshTelemetryPeriods, minimumFreshTelemetry):
		result.TelemetryState = TelemetryFresh
	case age <= max(interval*delayedTelemetryPeriods, minimumDelayedTelemetry):
		result.TelemetryState = TelemetryDelayed
	default:
		result.TelemetryState = TelemetryStale
	}
	return result
}
