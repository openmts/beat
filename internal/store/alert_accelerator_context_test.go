package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestAcceleratorAlertContextAndDisappearanceProof(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	states := NewAlertEvaluationStore(f.database.DB)
	key := AlertEvaluationKey{NodeID: f.node.ID, DeviceID: request.DeviceID}
	source, err := states.AcceleratorContext(t.Context(), key)
	if err != nil || source.Name == "" || source.Snapshot == nil {
		t.Fatal(source, err)
	}
	change := AlertEvaluationCommit{Decision: alerteval.StepResult{State: model.AlertEvaluationState{NodeID: key.NodeID, DeviceID: key.DeviceID, Reason: model.AcceleratorDeviceUnavailable}}, Policies: []AlertMetricPolicyReference{{AcceleratorDeviceID: key.DeviceID, AcceleratorSampleID: source.Snapshot.SampleID, AcceleratorPolicyRevision: source.Policy.Revision, AcceleratorDigest: source.Digest}}}
	if err := recheckAcceleratorEvaluation(t.Context(), f.database.DB, change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal("active device treated as disappeared", err)
	}
	absent := source
	native := *source.Snapshot
	native.Devices = nil
	absent.Snapshot = &native
	absent.Inactive = true
	if !absent.DeviceUnavailable(key.NodeID, key.DeviceID) {
		t.Fatal("successful absent vendor snapshot was not recognized")
	}
	native.State = "unavailable"
	if absent.DeviceUnavailable(key.NodeID, key.DeviceID) {
		t.Fatal("collector failure declared disappearance")
	}
	native.State = "ok"
	native.Capabilities = []model.AcceleratorCapability{{Vendor: "amd", State: "ok"}}
	if absent.DeviceUnavailable(key.NodeID, key.DeviceID) {
		t.Fatal("another vendor declared disappearance")
	}
	native.Capabilities = source.Snapshot.Capabilities
	native.Devices = source.Snapshot.Devices
	if absent.DeviceUnavailable(key.NodeID, key.DeviceID) {
		t.Fatal("present device declared disappearance")
	}
	if _, err := f.database.DB.Exec("UPDATE node_accelerator_state SET inventory_json='{}'"); err != nil {
		t.Fatal(err)
	}
	if _, err := states.AcceleratorContext(t.Context(), key); err == nil {
		t.Fatal("malformed inventory accepted")
	}
	if err := recheckAcceleratorEvaluation(t.Context(), f.database.DB, change); err == nil {
		t.Fatal("malformed commit metadata accepted")
	}
}

func TestAcceleratorTargetsRejectCapacityAndReadFailure(t *testing.T) {
	f, request := gpuEvidenceFixture(t)
	states := NewAlertEvaluationStore(f.database.DB)
	for ordinal := 2; ordinal <= 33; ordinal++ {
		fingerprint := fmt.Sprintf("%064x", ordinal)
		id := model.AcceleratorDeviceID(f.node.ID, fingerprint)
		if _, err := f.database.DB.Exec(`INSERT INTO node_accelerators(node_id,fingerprint,device_id,ordinal,vendor,model,capabilities_json,first_seen_at,last_seen_at) VALUES(?,?,?,?,'nvidia','GPU','[]',?,?)`, f.node.ID, fingerprint, id, ordinal, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := states.AcceleratorTargets(t.Context(), f.node.ID, model.AcceleratorAnyDevice); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	if err := f.database.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := states.AcceleratorTargets(t.Context(), f.node.ID, request.DeviceID); err == nil {
		t.Fatal("closed inventory looked empty")
	}
}
