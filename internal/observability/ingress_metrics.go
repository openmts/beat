package observability

import (
	"fmt"
	"strings"

	"github.com/beat/backend/internal/ingress"
)

// 入口准入指标只使用固定类别、范围与原因枚举，不携带任何客户端标识。
func (registry *Registry) SetIngressSnapshotSource(source func() ingress.Snapshot) {
	registry.mu.Lock()
	registry.ingressSource = source
	registry.mu.Unlock()
}

func (registry *Registry) refreshIngressSnapshot() ingress.Snapshot {
	registry.mu.RLock()
	source := registry.ingressSource
	registry.mu.RUnlock()
	if source == nil {
		return ingress.Snapshot{}
	}
	snapshot := source()
	registry.mu.Lock()
	registry.ingressStats = snapshot
	registry.mu.Unlock()
	return snapshot
}

func writeIngressMetrics(output *strings.Builder, snapshot ingress.Snapshot) {
	if len(snapshot.Classes) == 0 {
		return
	}
	output.WriteString("# TYPE beat_ingress_admitted_total counter\n")
	for _, class := range snapshot.Classes {
		fmt.Fprintf(output, "beat_ingress_admitted_total{class=%s} %d\n", quoteLabel(class.Class), class.Admitted)
	}
	output.WriteString("# TYPE beat_ingress_rejected_total counter\n")
	for _, class := range snapshot.Classes {
		for _, reason := range class.Reasons {
			fmt.Fprintf(output, "beat_ingress_rejected_total{class=%s,reason=%s} %d\n",
				quoteLabel(class.Class), quoteLabel(reason.Reason), reason.Count)
		}
	}
	output.WriteString("# TYPE beat_ingress_in_flight gauge\n")
	for _, class := range snapshot.Classes {
		fmt.Fprintf(output, "beat_ingress_in_flight{class=%s} %d\n", quoteLabel(class.Class), class.InFlight)
	}
	output.WriteString("# TYPE beat_ingress_identity_entries gauge\n")
	for _, class := range snapshot.Classes {
		fmt.Fprintf(output, "beat_ingress_identity_entries{class=%s} %d\n", quoteLabel(class.Class), class.IdentityEntries)
	}
	output.WriteString("# TYPE beat_ingress_identity_entries_limit gauge\n")
	fmt.Fprintf(output, "beat_ingress_identity_entries_limit %d\n", snapshot.IdentityEntriesLimit)
	output.WriteString("# TYPE beat_ingress_limit_requests_per_second gauge\n")
	output.WriteString("# TYPE beat_ingress_limit_burst gauge\n")
	output.WriteString("# TYPE beat_ingress_limit_in_flight gauge\n")
	for _, class := range snapshot.Classes {
		writeIngressLimitMetrics(output, class)
	}
}

func writeIngressLimitMetrics(output *strings.Builder, class ingress.ClassView) {
	label := quoteLabel(class.Class)
	fmt.Fprintf(output, "beat_ingress_limit_requests_per_second{class=%s,scope=\"global\"} %g\n", label, class.Limits.GlobalRatePerSecond)
	fmt.Fprintf(output, "beat_ingress_limit_requests_per_second{class=%s,scope=\"identity\"} %g\n", label, class.Limits.IdentityRatePerSecond)
	fmt.Fprintf(output, "beat_ingress_limit_burst{class=%s,scope=\"global\"} %d\n", label, class.Limits.GlobalBurst)
	fmt.Fprintf(output, "beat_ingress_limit_burst{class=%s,scope=\"identity\"} %d\n", label, class.Limits.IdentityBurst)
	fmt.Fprintf(output, "beat_ingress_limit_in_flight{class=%s,scope=\"global\"} %d\n", label, class.Limits.GlobalInFlight)
	fmt.Fprintf(output, "beat_ingress_limit_in_flight{class=%s,scope=\"identity\"} %d\n", label, class.Limits.IdentityInFlight)
}
