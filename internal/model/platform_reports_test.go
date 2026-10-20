package model

import (
	"strings"
	"testing"
)

func TestBackupUploadGeometryAndChunkSizes(t *testing.T) {
	// 空归档也要一个最小块，避免零长度会话无法校验。
	if chunkBytes, count, ok := BackupUploadGeometry(0); !ok || chunkBytes != MinimumBackupUploadChunkBytes || count != 1 {
		t.Fatalf("empty archive = %d %d %v", chunkBytes, count, ok)
	}
	if _, _, ok := BackupUploadGeometry(-1); ok {
		t.Fatal("negative size accepted")
	}
	if _, _, ok := BackupUploadGeometry(MaximumBackupUploadArchiveBytes + 1); ok {
		t.Fatal("oversized archive accepted")
	}
	// 小归档在 1 MiB 下限、大归档落在 64 MiB 上限，块数始终不超过 4096。
	for name, size := range map[string]int64{"one chunk": MinimumBackupUploadChunkBytes, "4 GiB": MaximumBackupUploadArchiveBytes} {
		chunkBytes, count, ok := BackupUploadGeometry(size)
		if !ok || chunkBytes < MinimumBackupUploadChunkBytes || chunkBytes > MaximumBackupUploadChunkBytes || count < 1 || count > MaximumBackupUploadChunks {
			t.Fatalf("%s geometry = %d %d %v", name, chunkBytes, count, ok)
		}
		if covered := int64(count) * chunkBytes; covered < size {
			t.Fatalf("%s coverage = %d < %d", name, covered, size)
		}
	}
	if offset := OffsetForBackupUploadChunk(2, MinimumBackupUploadChunkBytes); offset != 2*MinimumBackupUploadChunkBytes {
		t.Fatalf("offset = %d", offset)
	}
	if OffsetForBackupUploadChunk(-1, MinimumBackupUploadChunkBytes) != -1 ||
		OffsetForBackupUploadChunk(1, 0) != -1 {
		t.Fatal("invalid offset accepted")
	}
	// 最后一块取剩余字节；越过声明长度的块长度为 0。
	chunk := int64(MinimumBackupUploadChunkBytes)
	if size := BackupUploadChunkSize(0, chunk, chunk+123); size != chunk {
		t.Fatalf("full chunk = %d", size)
	}
	if size := BackupUploadChunkSize(1, chunk, chunk+123); size != 123 {
		t.Fatalf("tail chunk = %d", size)
	}
	if size := BackupUploadChunkSize(2, chunk, chunk+123); size != 0 {
		t.Fatalf("past end = %d", size)
	}
	for name, size := range map[string]int64{
		"negative index": BackupUploadChunkSize(-1, chunk, chunk),
		"index too high": BackupUploadChunkSize(MaximumBackupUploadChunks, chunk, chunk),
		"zero chunk":     BackupUploadChunkSize(0, 0, chunk),
		"negative size":  BackupUploadChunkSize(0, chunk, -1),
	} {
		if size != -1 {
			t.Fatalf("%s = %d", name, size)
		}
	}
}

func TestApplicationRetentionSettingsValidation(t *testing.T) {
	settings := DefaultApplicationRetentionSettings()
	if settings.AlertEventDays != 180 || settings.AuditEventDays != 180 || settings.CleanupHourUTC != 3 || !settings.AutoCleanupEnabled {
		t.Fatalf("defaults = %+v", settings)
	}
	if err := settings.Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*ApplicationRetentionSettings)
	}{
		{"alert days below minimum", func(value *ApplicationRetentionSettings) { value.AlertEventDays = ApplicationRetentionMinimumDays - 1 }},
		{"alert days above maximum", func(value *ApplicationRetentionSettings) { value.AlertEventDays = ApplicationRetentionMaximumDays + 1 }},
		{"audit days below minimum", func(value *ApplicationRetentionSettings) { value.AuditEventDays = 0 }},
		{"audit days above maximum", func(value *ApplicationRetentionSettings) { value.AuditEventDays = ApplicationRetentionMaximumDays + 1 }},
		{"cleanup hour negative", func(value *ApplicationRetentionSettings) { value.CleanupHourUTC = -1 }},
		{"cleanup hour above range", func(value *ApplicationRetentionSettings) { value.CleanupHourUTC = 24 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			value := DefaultApplicationRetentionSettings()
			testCase.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatalf("settings accepted: %+v", value)
			}
		})
	}
	bounds := DefaultApplicationRetentionSettings()
	bounds.AlertEventDays, bounds.AuditEventDays = ApplicationRetentionMinimumDays, ApplicationRetentionMaximumDays
	bounds.CleanupHourUTC = 23
	if err := bounds.Validate(); err != nil {
		t.Fatalf("boundary settings rejected: %v", err)
	}
}

func TestDashboardThresholdModeValidation(t *testing.T) {
	timeseries := DashboardPanel{Type: "timeseries"}
	for _, mode := range []string{"", ThresholdModeOff, ThresholdModeLine, ThresholdModeArea, ThresholdModeBackground} {
		panel := timeseries
		panel.ThresholdMode = mode
		if err := validateDashboardThresholdMode(panel); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	broken := timeseries
	broken.ThresholdMode = "sparkle"
	if err := validateDashboardThresholdMode(broken); err == nil {
		t.Fatal("unknown mode accepted")
	}
	for _, mode := range []string{ThresholdModeLine, ThresholdModeArea, ThresholdModeBackground} {
		panel := DashboardPanel{Type: "stat", ThresholdMode: mode}
		if err := validateDashboardThresholdMode(panel); err == nil {
			t.Fatalf("stat panel accepted %q", mode)
		}
	}
	off := DashboardPanel{Type: "stat", ThresholdMode: ThresholdModeOff}
	if err := validateDashboardThresholdMode(off); err != nil {
		t.Fatalf("stat off rejected: %v", err)
	}
}

func TestDashboardVersionSummaryValidation(t *testing.T) {
	if err := ValidateDashboardVersionSummary(""); err != nil {
		t.Fatalf("empty summary rejected: %v", err)
	}
	if err := ValidateDashboardVersionSummary("演练恢复：回到 CPU 面板"); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}
	if err := ValidateDashboardVersionSummary(strings.Repeat("a", MaxDashboardVersionSummaryBytes)); err != nil {
		t.Fatalf("boundary summary rejected: %v", err)
	}
	if err := ValidateDashboardVersionSummary(strings.Repeat("a", MaxDashboardVersionSummaryBytes+1)); err == nil {
		t.Fatal("oversized summary accepted")
	}
	if err := ValidateDashboardVersionSummary("bad\x00summary"); err == nil {
		t.Fatal("control character accepted")
	}
	if err := ValidateDashboardVersionSummary(string([]byte{0xff, 0xfe})); err == nil {
		t.Fatal("invalid utf-8 accepted")
	}
}
