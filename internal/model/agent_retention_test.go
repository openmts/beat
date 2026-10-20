package model

import (
	"errors"
	"testing"
)

func TestAgentRetentionSettingsValidateBounds(t *testing.T) {
	valid := []AgentRetentionSettings{
		{},
		{RolloutDays: 1, ReleaseDays: MaximumReleaseRetentionDays},
		{RolloutDays: MaximumReleaseRetentionDays, ReleaseDays: 0},
	}
	for _, settings := range valid {
		if err := settings.Validate(); err != nil {
			t.Fatalf("settings %+v rejected: %v", settings, err)
		}
	}
	invalid := []AgentRetentionSettings{
		{RolloutDays: -1},
		{ReleaseDays: -1},
		{RolloutDays: MaximumReleaseRetentionDays + 1},
		{ReleaseDays: MaximumReleaseRetentionDays + 1},
	}
	for _, settings := range invalid {
		if err := settings.Validate(); !errors.Is(err, ErrAgentRetentionInvalid) {
			t.Fatalf("settings %+v accepted: %v", settings, err)
		}
	}
	if !ValidRetentionDays(0) || !ValidRetentionDays(MaximumReleaseRetentionDays) ||
		ValidRetentionDays(-1) || ValidRetentionDays(MaximumReleaseRetentionDays+1) {
		t.Fatal("ValidRetentionDays bounds drifted")
	}
}

func TestAgentRetentionSettingsEnabledOnlyWhenPeriodsExist(t *testing.T) {
	cases := map[AgentRetentionSettings]bool{
		{}:                                false,
		{Configured: true}:                false,
		{RolloutDays: 1}:                  true,
		{ReleaseDays: 30}:                 true,
		{RolloutDays: 1, ReleaseDays: 30}: true,
	}
	for settings, want := range cases {
		if got := settings.Enabled(); got != want {
			t.Fatalf("settings %+v enabled = %v, want %v", settings, got, want)
		}
	}
}
