package testutil

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAcceleratorReportIsSynthetic(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	report := AcceleratorReport(at)
	if report.Version == 0 || !report.CapturedAt.Equal(at) || len(report.Devices) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Devices[0].Metrics["gpu_utilization_percent"] != 73.125 || report.Capabilities[0].Vendor != "nvidia" {
		t.Fatalf("device = %+v", report.Devices[0])
	}
}

func TestCountryMMDBEmitsMaxMindMetadata(t *testing.T) {
	data := CountryMMDB("ZZ", 1_700_000_000)
	// 合成库把元数据追加在搜索树之后，标记只需出现在数据区里，不要求位于末尾。
	if !bytes.Contains(data, []byte("\xab\xcd\xefMaxMind.com")) {
		t.Fatal("synthetic country database is missing the MaxMind marker")
	}
	for _, expected := range [][]byte{[]byte("ZZ"), []byte("iso_code"), []byte("GeoLite2-Country"), []byte("Synthetic fixture")} {
		if !bytes.Contains(data, expected) {
			t.Fatalf("synthetic country database is missing %q", expected)
		}
	}
}

func TestVersionedAgentReportPassesThroughUnusableBodies(t *testing.T) {
	for name, body := range map[string]string{
		"malformed":         "not json",
		"null object":       "null",
		"already versioned": `{"protocol_version":2,"sample_id":"c4b3f1f1-2a8d-4d1f-9c59-1a3a5e0d0f11"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := VersionedAgentReport(t, body); got != body {
				t.Fatalf("body = %q", got)
			}
		})
	}
}

func TestVersionedAgentReportAddsProtocolIdentity(t *testing.T) {
	updated := VersionedAgentReport(t, `{"name":"node","metrics":{"cpu":1},"custom_metrics":{"load1":1.5}}`)
	var fields map[string]any
	if err := json.Unmarshal([]byte(updated), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["protocol_version"] != float64(model.AgentReportProtocol) || fields["report_interval_ms"] != float64(1000) {
		t.Fatalf("fields = %+v", fields)
	}
	for _, key := range []string{"sample_id", "captured_at", "custom_samples"} {
		if _, present := fields[key]; !present {
			t.Fatalf("fields are missing %s: %+v", key, fields)
		}
	}
	sources, ok := fields["custom_samples"].(map[string]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("custom_samples = %+v", fields["custom_samples"])
	}
}

func TestVersionedCustomAgentReportRewritesCustomSamples(t *testing.T) {
	definitions := []model.CustomMetric{{ID: "fixture-load", Name: "load1", ShellCommand: "printf 1", IntervalSeconds: 5}}
	updated := VersionedCustomAgentReport(t, `{"custom_metrics":{"load1":2.5,"unused":1}}`, definitions)
	var report model.AgentReport
	if err := json.Unmarshal([]byte(updated), &report); err != nil {
		t.Fatal(err)
	}
	metadata, present := report.CustomSamples["load1"]
	if !present || metadata.CadenceMS != 5000 {
		t.Fatalf("custom samples = %+v", report.CustomSamples)
	}
	if _, present := report.CustomSamples["unused"]; present {
		t.Fatalf("unregistered sample was kept: %+v", report.CustomSamples)
	}
}

func TestCustomSourceFixtureDerivesCadence(t *testing.T) {
	at := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	metadata := CustomSourceFixture(at, model.CustomMetric{Name: "load1", ShellCommand: "printf 1", IntervalSeconds: 30})
	if !metadata.CapturedAt.Equal(at) || metadata.CadenceMS != 30000 || metadata.SampleID == "" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestAgentTestIdentityReportsCredentialMode(t *testing.T) {
	active := model.Node{ID: "node-1", Name: "node", AgentTokenHash: bytes.Repeat([]byte{3}, 32),
		AgentTokenCreatedAt: pointerTo(time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC))}
	identity := AgentTestIdentity(active)
	if identity.Mode != model.AgentCredentialActive || identity.CredentialFingerprint == "" {
		t.Fatalf("identity = %+v", identity)
	}
	legacy := model.Node{ID: "node-2", Name: "legacy"}
	legacyIdentity := AgentTestIdentity(legacy)
	if legacyIdentity.Mode != model.AgentCredentialLegacy ||
		legacyIdentity.CredentialFingerprint != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("legacy identity = %+v", legacyIdentity)
	}
}

func pointerTo(value time.Time) *time.Time { return &value }
