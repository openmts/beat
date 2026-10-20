package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// securityMetricsSource 提供登录安全计数、持久化规模与出箱延迟。
type securityMetricsSource interface {
	SecurityMetricsSnapshot() adminauth.SecurityMetricsSnapshot
	SecurityMetrics(context.Context) (store.SecurityDiagnostics, error)
	SecurityOutboxAge(context.Context, time.Time) (float64, bool, error)
}

func (operations *operability) loginSecurityCheck(ctx context.Context) readinessCheck {
	source := operations.security
	if source == nil {
		return readinessCheck{Status: "ok"}
	}
	if _, err := source.SecurityMetrics(ctx); err != nil {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) writeSecurityMetrics(output *strings.Builder, ctx context.Context) {
	source := operations.security
	if source == nil {
		return
	}
	writeSecurityCounters(output, source.SecurityMetricsSnapshot())
	operations.writeSecurityGauges(output, ctx, source)
}

// writeSecurityCounters 只使用固定词表标签，保证序列可枚举且不泄漏原始身份。
func writeSecurityCounters(output *strings.Builder, snapshot adminauth.SecurityMetricsSnapshot) {
	_, _ = output.WriteString("# TYPE beat_security_login_events_total counter\n")
	for _, kind := range []string{model.SecurityEventLoginSucceeded, model.SecurityEventLoginSuspicious} {
		_, _ = fmt.Fprintf(output, "beat_security_login_events_total{kind=%q} %d\n", kind, snapshot.Kinds[kind])
	}
	for _, severity := range []string{model.SecuritySeverityInfo, model.SecuritySeverityMedium, model.SecuritySeverityHigh} {
		_, _ = fmt.Fprintf(output, "beat_security_login_events_total{severity=%q} %d\n", severity, snapshot.Severities[severity])
	}
	_, _ = output.WriteString("# TYPE beat_security_login_suspicious_total counter\n")
	for _, reason := range model.SecurityReasonCodes() {
		_, _ = fmt.Fprintf(output, "beat_security_login_suspicious_total{reason=%q} %d\n", reason, snapshot.Reasons[reason])
	}
	_, _ = output.WriteString("# TYPE beat_security_transaction_failures_total counter\n")
	_, _ = fmt.Fprintf(output, "beat_security_transaction_failures_total %d\n", snapshot.TransactionFailures)
	_, _ = output.WriteString("# TYPE beat_security_outbox_failures_total counter\n")
	_, _ = fmt.Fprintf(output, "beat_security_outbox_failures_total %d\n", snapshot.OutboxFailures)
}

func (operations *operability) writeSecurityGauges(
	output *strings.Builder, ctx context.Context, source securityMetricsSource,
) {
	diagnostics, err := source.SecurityMetrics(ctx)
	if err != nil {
		return
	}
	_, _ = output.WriteString("# TYPE beat_security_threshold_rows gauge\n")
	_, _ = fmt.Fprintf(output, "beat_security_threshold_rows %d\n", diagnostics.Thresholds)
	_, _ = output.WriteString("# TYPE beat_security_fingerprint_rows gauge\n")
	_, _ = fmt.Fprintf(output, "beat_security_fingerprint_rows %d\n", diagnostics.Fingerprints)
	age, found, err := source.SecurityOutboxAge(ctx, time.Now().UTC())
	if err != nil || !found {
		return
	}
	_, _ = output.WriteString("# TYPE beat_security_oldest_pending_seconds gauge\n")
	_, _ = fmt.Fprintf(output, "beat_security_oldest_pending_seconds %g\n", age)
}

// securityMetricsSourceFrom 只在认证服务可用时暴露安全指标。
func securityMetricsSourceFrom(services *serverServices) securityMetricsSource {
	if services == nil || services.security == nil {
		return nil
	}
	return services.security
}
