package model

import "testing"

func TestMaintenanceSettingsValidation(t *testing.T) {
	valid := DefaultMaintenanceSettings()
	if err := valid.Validate(); err != nil {
		t.Fatalf("default settings: %v", err)
	}

	tests := []MaintenanceSettings{
		{RetentionDays: 0, CleanupHourUTC: 3},
		{RetentionDays: 3651, CleanupHourUTC: 3},
		{RetentionDays: 30, CleanupHourUTC: -1},
		{RetentionDays: 30, CleanupHourUTC: 24},
	}
	for _, settings := range tests {
		if err := settings.Validate(); err == nil {
			t.Fatalf("Validate(%+v) = nil, want error", settings)
		}
	}
}
