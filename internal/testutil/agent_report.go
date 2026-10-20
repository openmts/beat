package testutil

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// VersionedAgentReport 给已有边界测试补协议身份，保留原始指标/元数据错误。
func VersionedAgentReport(t *testing.T, body string) string {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil || fields == nil {
		return body
	}
	if _, present := fields["protocol_version"]; present {
		return body
	}
	fields["protocol_version"], fields["sample_id"] = model.AgentReportProtocol, uuid.NewString()
	at := time.Now().UTC()
	fields["captured_at"], fields["report_interval_ms"] = at, 1000
	if values, ok := fields["custom_metrics"].(map[string]any); ok {
		sources := make(map[string]model.CustomSampleMetadata)
		for name := range values {
			sources[name] = CustomSourceFixture(at, model.CustomMetric{ID: "fixture", Name: name, ShellCommand: "printf 1", IntervalSeconds: 5})
		}
		fields["custom_samples"] = sources
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func AgentTestIdentity(node model.Node) model.AgentIdentity {
	fingerprint := model.AgentCredentialFingerprint(node.AgentTokenHash)
	if node.AgentCredentialStatus() == model.AgentCredentialLegacy {
		fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	return model.AgentIdentity{NodeID: node.ID, NodeName: node.Name, Mode: node.AgentCredentialStatus(), CredentialFingerprint: fingerprint}
}
