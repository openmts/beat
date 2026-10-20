package store

import "github.com/beat/backend/internal/model"

func publicTelemetryEvidence(before, after model.PublicTelemetryPolicy) metadataEvidence {
	fields := []string{}
	changes := []struct {
		name    string
		changed bool
	}{
		{"enabled", before.Enabled != after.Enabled},
		{"allowed_events_json", !equalPublicTelemetryEvents(before.AllowedEvents, after.AllowedEvents)},
		{"sample_ceiling", before.SampleCeiling != after.SampleCeiling},
		{"retention_days", before.RetentionDays != after.RetentionDays},
	}
	for _, change := range changes {
		if change.changed {
			fields = append(fields, change.name)
		}
	}
	return metadataEvidence{Action: "public_telemetry.update", Type: "public_telemetry", ID: "global", Fields: fields}
}

// publicTelemetryPolicyEqual 只比较持久化列覆盖到的字段。
func publicTelemetryPolicyEqual(first, second model.PublicTelemetryPolicy) bool {
	return first.Enabled == second.Enabled && first.SampleCeiling == second.SampleCeiling &&
		first.RetentionDays == second.RetentionDays && first.Revision == second.Revision &&
		first.UpdatedBy == second.UpdatedBy && first.UpdatedAt.Equal(second.UpdatedAt) &&
		equalPublicTelemetryEvents(first.AllowedEvents, second.AllowedEvents)
}

func equalPublicTelemetryEvents(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
