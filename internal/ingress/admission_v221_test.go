package ingress

import (
	"net/http"
	"testing"
	"time"
)

// New 是启动期唯一的构造入口：合法容量放行，非法容量必须在启动期失败而不是静默降级。
func TestNewAcceptsDefaultCapacityAndRejectsInvalid(t *testing.T) {
	limiter, err := New(DefaultCapacity())
	if err != nil {
		t.Fatalf("expected the default capacity to be accepted, got %v", err)
	}
	if limiter == nil {
		t.Fatal("expected a limiter for the default capacity")
	}
	if _, err := New(Capacity{}); err == nil {
		t.Fatal("expected the zero capacity to be rejected")
	}
}

// Rejection.Error 只暴露稳定原因标签，不携带任何客户端标识。
func TestRejectionErrorCarriesOnlyTheReason(t *testing.T) {
	rejection := newRejection(ReasonTableFull, http.StatusTooManyRequests, tableFullRetryAfter)
	if got, want := rejection.Error(), "ingress rejected: table_full"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if rejection.RetryAfter != tableFullRetryAfter {
		t.Fatalf("expected retry-after %d, got %d", tableFullRetryAfter, rejection.RetryAfter)
	}
}

// 公共指标工作区的匿名读走公共静态预算，只有区间查询是公共历史预算。
func TestClassifyRoutesPublicMetricWorkspace(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		class  Class
	}{
		{"catalog", http.MethodGet, "/api/v1/metrics/catalog", PublicStatic},
		{"query read", http.MethodGet, "/api/v1/metrics/query", PublicStatic},
		{"query write", http.MethodPost, "/api/v1/metrics/query", PublicHistory},
		{"dashboards", http.MethodGet, "/api/v1/metrics/dashboards/default", PublicStatic},
		{"annotations", http.MethodGet, "/api/v1/metrics/annotations", PublicStatic},
		{"snapshot read", http.MethodGet, "/api/v1/metrics/snapshots/board-1", PublicStatic},
		{"snapshot write", http.MethodPost, "/api/v1/metrics/snapshots/board-1", AdminWrite},
		{"snapshot write fallback", http.MethodPost, "/api/v1/nodes/node-1", AdminWrite},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			class, ok := Classify(testCase.method, testCase.path)
			if !ok {
				t.Fatalf("expected %s %s to be admitted", testCase.method, testCase.path)
			}
			if class != testCase.class {
				t.Fatalf("expected class %s, got %s", testCase.class, class)
			}
		})
	}
}

func TestClassStringAndRetryAfterBoundaries(t *testing.T) {
	if got := Class(200).String(); got != "unknown" {
		t.Fatalf("expected an unknown class label, got %q", got)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	cases := []struct {
		name   string
		bucket bucket
		rate   Rate
		want   int
	}{
		{"already available", bucket{tokens: 5, updated: now}, Rate{PerSecond: 1, Burst: 5}, 1},
		{"single second", bucket{tokens: 0, updated: now}, Rate{PerSecond: 1, Burst: 1}, 1},
		{"clamped", bucket{tokens: 0, updated: now}, Rate{PerSecond: 0.001, Burst: 1}, maximumRetryAfter},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.bucket.retryAfter(testCase.rate); got != testCase.want {
				t.Fatalf("expected %d, got %d", testCase.want, got)
			}
		})
	}
}

// 无效类别不接受准入；零值租约的释放必须是幂等的空操作。
func TestAdmitRejectsUnknownClassAndReleaseIsIdempotent(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0).UTC()}
	limiter := newTestLimiter(t, DefaultCapacity(), clock)
	lease, rejection := limiter.Admit(Class(200), "identity")
	if rejection != nil {
		t.Fatalf("expected no rejection for an unknown class, got %v", rejection)
	}
	lease.Release()
	lease.Release()

	admitted, err := limiter.Admit(AgentRead, "identity")
	if err != nil || admitted.limiter == nil {
		t.Fatalf("expected a valid lease, got %v", err)
	}
	admitted.Release()
	admitted.Release()
}

// 环境覆盖必须拒绝非数值、越界与互不一致的取值，不能静默关闭保护。
func TestFromEnvironmentRejectsMalformedOverrides(t *testing.T) {
	rejected := map[string]map[string]string{
		"rate is not a number":     {"BEAT_INGRESS_PUBLIC_STATIC_GLOBAL_RATE": "abc"},
		"burst is not an integer":  {"BEAT_INGRESS_PUBLIC_STATIC_GLOBAL_BURST": "abc"},
		"rate is out of range":     {"BEAT_INGRESS_PUBLIC_STATIC_GLOBAL_RATE": "0"},
		"burst is out of range":    {"BEAT_INGRESS_PUBLIC_STATIC_GLOBAL_BURST": "0"},
		"identity entries invalid": {"BEAT_INGRESS_IDENTITY_ENTRIES": "abc"},
		"identity ttl invalid":     {"BEAT_INGRESS_IDENTITY_TTL": "abc"},
		"identity rate above global": {
			"BEAT_INGRESS_PUBLIC_STATIC_GLOBAL_RATE": "1", "BEAT_INGRESS_PUBLIC_STATIC_IDENTITY_RATE": "10",
		},
	}
	for name, overrides := range rejected {
		t.Run(name, func(t *testing.T) {
			if _, err := FromEnvironment(func(key string) string { return overrides[key] }); err == nil {
				t.Fatalf("expected %v to be rejected", overrides)
			}
		})
	}
	capacity, err := FromEnvironment(func(key string) string {
		if key == "BEAT_INGRESS_PUBLIC_STATIC_GLOBAL_RATE" {
			return "500"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("expected a valid override to be accepted, got %v", err)
	}
	if got := capacity.Policies[PublicStatic].GlobalRate.PerSecond; got != 500 {
		t.Fatalf("expected the override to be applied, got %g", got)
	}
}
