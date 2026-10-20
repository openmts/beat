package observability

import (
	"strings"
	"testing"

	"github.com/beat/backend/internal/ingress"
)

func ingressSnapshotFixture() ingress.Snapshot {
	return ingress.Snapshot{
		IdentityEntries:      7,
		IdentityEntriesLimit: 1024,
		IdentityTTLSeconds:   60,
		Classes: []ingress.ClassView{
			{
				Class: "AdminExpensive", Admitted: 11, Rejected: 3, InFlight: 2, IdentityEntries: 5,
				Reasons: []ingress.ReasonCount{{Reason: "budget", Count: 2}, {Reason: "in_flight", Count: 1}},
				Limits: ingress.Limits{GlobalRatePerSecond: 1, GlobalBurst: 2, GlobalInFlight: 4,
					IdentityRatePerSecond: 0.5, IdentityBurst: 1, IdentityInFlight: 2},
			},
			{Class: "Report", Admitted: 42},
		},
	}
}

func TestRegistryIngressMetricsDefaultToEmptySnapshot(t *testing.T) {
	registry := NewRegistry()
	snapshot := registry.refreshIngressSnapshot()
	if len(snapshot.Classes) != 0 || snapshot.IdentityEntries != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if metrics := registry.Prometheus(); strings.Contains(metrics, "beat_ingress_admitted_total") {
		t.Fatalf("empty snapshot emitted ingress metrics:\n%s", metrics)
	}
}

func TestRegistryIngressMetricsFollowSnapshotSource(t *testing.T) {
	registry := NewRegistry()
	calls := 0
	registry.SetIngressSnapshotSource(func() ingress.Snapshot {
		calls++
		return ingressSnapshotFixture()
	})
	metrics := registry.Prometheus()
	if calls != 1 {
		t.Fatalf("source calls = %d", calls)
	}
	for _, expected := range []string{
		"# TYPE beat_ingress_admitted_total counter",
		`beat_ingress_admitted_total{class="AdminExpensive"} 11`,
		`beat_ingress_rejected_total{class="AdminExpensive",reason="budget"} 2`,
		`beat_ingress_rejected_total{class="AdminExpensive",reason="in_flight"} 1`,
		`beat_ingress_in_flight{class="AdminExpensive"} 2`,
		`beat_ingress_identity_entries{class="AdminExpensive"} 5`,
		"beat_ingress_identity_entries_limit 1024",
		`beat_ingress_limit_requests_per_second{class="AdminExpensive",scope="global"} 1`,
		`beat_ingress_limit_requests_per_second{class="AdminExpensive",scope="identity"} 0.5`,
		`beat_ingress_limit_burst{class="AdminExpensive",scope="global"} 2`,
		`beat_ingress_limit_burst{class="AdminExpensive",scope="identity"} 1`,
		`beat_ingress_limit_in_flight{class="AdminExpensive",scope="global"} 4`,
		`beat_ingress_limit_in_flight{class="AdminExpensive",scope="identity"} 2`,
		`beat_ingress_admitted_total{class="Report"} 42`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, metrics)
		}
	}
	if registry.ingressStats.IdentityEntries != 7 {
		t.Fatalf("cached snapshot = %+v", registry.ingressStats)
	}
}

func TestWriteIngressMetricsSkipsEmptyClasses(t *testing.T) {
	output := &strings.Builder{}
	writeIngressMetrics(output, ingress.Snapshot{IdentityEntriesLimit: 16})
	if output.Len() != 0 {
		t.Fatalf("output = %q", output.String())
	}
}

func TestWriteIngressMetricsEscapesLabels(t *testing.T) {
	output := &strings.Builder{}
	writeIngressMetrics(output, ingress.Snapshot{Classes: []ingress.ClassView{{
		Class: "weird\"class", Reasons: []ingress.ReasonCount{{Reason: "a\\b", Count: 1}},
	}}})
	if !strings.Contains(output.String(), `beat_ingress_rejected_total{class="weird\"class",reason="a\\b"} 1`) {
		t.Fatalf("output = %q", output.String())
	}
}
