package model

import (
	"errors"
	"strings"
	"testing"
)

func TestMetricErasureScopeHelpers(t *testing.T) {
	scopes := MetricErasureScopes()
	if len(scopes) != 6 {
		t.Fatalf("scopes = %v", scopes)
	}
	for index := 1; index < len(scopes); index += 1 {
		if scopes[index-1] >= scopes[index] {
			t.Fatalf("scopes not sorted: %v", scopes)
		}
	}
	for _, scope := range scopes {
		if !ValidMetricErasureScope(scope) {
			t.Fatalf("%s rejected", scope)
		}
	}
	if ValidMetricErasureScope("measurement") || ValidMetricErasureScope("") {
		t.Fatal("unknown scope accepted")
	}
	cases := map[string]string{
		MetricErasureNodeHistory:        MetricErasureTargetNode,
		MetricErasureDeleteNode:         MetricErasureTargetNode,
		MetricErasureNetworkTaskHistory: MetricErasureTargetNetworkTask,
		MetricErasureDeleteNetworkTask:  MetricErasureTargetNetworkTask,
		MetricErasureFleetTelemetry:     MetricErasureTargetNone,
		MetricErasureAllMetricHistory:   MetricErasureTargetNone,
	}
	for scope, targetType := range cases {
		if actual := MetricErasureTargetTypeFor(scope); actual != targetType {
			t.Fatalf("%s target type = %q", scope, actual)
		}
	}
	if MetricErasureConfirmationFor(MetricErasureFleetTelemetry) != MetricErasureFleetConfirmation ||
		MetricErasureConfirmationFor(MetricErasureAllMetricHistory) != MetricErasureAllConfirmation {
		t.Fatal("fleet confirmation mismatch")
	}
	if MetricErasureConfirmationFor(MetricErasureDeleteNode) != "" {
		t.Fatal("entity scope must use the exact display name")
	}
}

func TestMetricErasureJobStateHelpers(t *testing.T) {
	for _, state := range []string{MetricErasureQueued, MetricErasureClaimed, MetricErasureDeleting,
		MetricErasureCompacting, MetricErasureFinalizing} {
		if !(MetricErasureJob{State: state}).Active() {
			t.Fatalf("%s not active", state)
		}
	}
	for _, state := range []string{MetricErasureSucceeded, MetricErasureFailed, MetricErasureCanceled} {
		if (MetricErasureJob{State: state}).Active() {
			t.Fatalf("%s active", state)
		}
	}
	if !(MetricErasureJob{State: MetricErasureSucceeded}).Succeeded() ||
		(MetricErasureJob{State: MetricErasureFailed}).Succeeded() {
		t.Fatal("succeeded helper mismatch")
	}
	if !(MetricErasureJob{Scope: MetricErasureDeleteNode}).ErasesEntity() ||
		!(MetricErasureJob{Scope: MetricErasureDeleteNetworkTask}).ErasesEntity() {
		t.Fatal("entity deletion scopes must erase application rows")
	}
	if (MetricErasureJob{Scope: MetricErasureNodeHistory}).ErasesEntity() {
		t.Fatal("history-only scope must keep the node row")
	}
}

func TestMetricErasureCreateRequestValidation(t *testing.T) {
	requestID := "6f1b6d2e-9c1f-4d5a-8a2b-4f0e1d2c3b4a"
	fleet := MetricErasureCreateRequest{Scope: MetricErasureFleetTelemetry, RequestID: requestID,
		Confirmation: MetricErasureFleetConfirmation}
	if err := fleet.Validate(); err != nil {
		t.Fatalf("fleet request rejected: %v", err)
	}
	all := MetricErasureCreateRequest{Scope: MetricErasureAllMetricHistory, RequestID: requestID,
		Confirmation: MetricErasureAllConfirmation}
	if err := all.Validate(); err != nil {
		t.Fatalf("all-history request rejected: %v", err)
	}
	node := MetricErasureCreateRequest{Scope: MetricErasureDeleteNode, TargetID: "node-1", RequestID: requestID,
		Confirmation: "edge router", TargetName: "edge router"}
	if err := node.Validate(); err != nil {
		t.Fatalf("node request rejected: %v", err)
	}
	cases := []struct {
		name    string
		request MetricErasureCreateRequest
	}{
		{"unknown scope", MetricErasureCreateRequest{Scope: "measurement", RequestID: requestID}},
		{"invalid request id", MetricErasureCreateRequest{Scope: MetricErasureFleetTelemetry, RequestID: "not-a-uuid",
			Confirmation: MetricErasureFleetConfirmation}},
		{"fleet with target", MetricErasureCreateRequest{Scope: MetricErasureFleetTelemetry, TargetID: "node-1", RequestID: requestID,
			Confirmation: MetricErasureFleetConfirmation}},
		{"fleet wrong confirmation", MetricErasureCreateRequest{Scope: MetricErasureFleetTelemetry, RequestID: requestID,
			Confirmation: "erase fleet"}},
		{"entity without target", MetricErasureCreateRequest{Scope: MetricErasureDeleteNode, RequestID: requestID,
			Confirmation: "edge router", TargetName: "edge router"}},
		{"entity target id too long", MetricErasureCreateRequest{Scope: MetricErasureDeleteNode,
			TargetID: strings.Repeat("i", MaximumMetricErasureIDBytes+1), RequestID: requestID,
			Confirmation: "edge router", TargetName: "edge router"}},
		{"entity without display name", MetricErasureCreateRequest{Scope: MetricErasureDeleteNode, TargetID: "node-1",
			RequestID: requestID, Confirmation: "edge router"}},
		{"entity display name too long", MetricErasureCreateRequest{Scope: MetricErasureDeleteNode, TargetID: "node-1",
			RequestID: requestID, TargetName: strings.Repeat("n", MaximumMetricErasureNameBytes+1),
			Confirmation: strings.Repeat("n", MaximumMetricErasureNameBytes+1)}},
		{"entity display name with nul", MetricErasureCreateRequest{Scope: MetricErasureDeleteNode, TargetID: "node-1",
			RequestID: requestID, TargetName: "edge\x00router", Confirmation: "edge\x00router"}},
		{"entity confirmation mismatch", MetricErasureCreateRequest{Scope: MetricErasureDeleteNetworkTask, TargetID: "task-1",
			RequestID: requestID, TargetName: "task one", Confirmation: "task two"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.request.Validate(); !errors.Is(err, ErrMetricErasureInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestMetricErasureDigestValidation(t *testing.T) {
	digest := strings.Repeat("ab", MaximumMetricErasureDigestBytes/2)
	if !ValidMetricErasureDigest(digest) {
		t.Fatal("lowercase hex digest rejected")
	}
	for name, value := range map[string]string{
		"too short": digest[:MaximumMetricErasureDigestBytes-1],
		"too long":  digest + "a",
		"uppercase": strings.ToUpper(digest),
		"non hex":   strings.Repeat("z", MaximumMetricErasureDigestBytes),
		"empty":     "",
	} {
		if ValidMetricErasureDigest(value) {
			t.Fatalf("%s digest accepted", name)
		}
	}
}
