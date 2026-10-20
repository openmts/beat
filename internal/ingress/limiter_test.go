package ingress

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (clock *fakeClock) advance(duration time.Duration) {
	clock.now = clock.now.Add(duration)
}

func newTestLimiter(t *testing.T, capacity Capacity, clock *fakeClock) *Limiter {
	t.Helper()
	if err := capacity.Validate(); err != nil {
		t.Fatalf("test capacity must be valid: %v", err)
	}
	return newLimiter(capacity, func() time.Time { return clock.now })
}

func TestClassifyMapsFixedRouteClasses(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		class  Class
		ok     bool
	}{
		{"login", http.MethodPost, "/api/v1/auth/login", AuthKDF, true},
		{"reauth", http.MethodPost, "/api/v1/auth/reauthenticate", AuthKDF, true},
		{"password", http.MethodPut, "/api/v1/admin/users/me/password", AuthKDF, true},
		{"node report", http.MethodPost, "/api/v1/nodes/report", AgentWrite, true},
		{"network results", http.MethodPost, "/api/v1/network/results", AgentWrite, true},
		{"assignments", http.MethodGet, "/api/v1/network/assignments", AgentRead, true},
		{"agent channel", http.MethodGet, "/api/v1/agents/ws", 0, false},
		{"public metrics channel", http.MethodGet, "/api/v1/ws/metrics", 0, false},
		{"site settings", http.MethodGet, "/api/v1/settings/site", PublicStatic, true},
		{"site update", http.MethodPut, "/api/v1/settings/site", AdminWrite, true},
		{"node manage", http.MethodGet, "/api/v1/nodes/manage", AdminRead, true},
		{"node install", http.MethodGet, "/api/v1/nodes/abc/install", AdminRead, true},
		{"custom metrics", http.MethodGet, "/api/v1/custom-metrics", AdminRead, true},
		{"ssh keys", http.MethodGet, "/api/v1/ssh-keys", AdminRead, true},
		{"theme active", http.MethodGet, "/api/v1/themes/active", PublicStatic, true},
		{"network quality", http.MethodGet, "/api/v1/network/quality", PublicSnapshot, true},
		{"network history", http.MethodGet, "/api/v1/network/quality/task/history", PublicHistory, true},
		{"interactions", http.MethodPost, "/api/v1/public/interactions", PublicStatic, true},
		{"public history", http.MethodGet, "/api/v1/nodes/abc/history", PublicHistory, true},
		{"public node", http.MethodGet, "/api/v1/nodes/abc", PublicSnapshot, true},
		{"admin read", http.MethodGet, "/api/v1/admin/nodes", AdminRead, true},
		{"admin write", http.MethodPut, "/api/v1/settings/site", AdminWrite, true},
		{"admin upload", http.MethodPost, "/api/v1/admin/backups/uploads", AdminExpensive, true},
		{"admin export", http.MethodGet, "/api/v1/admin/themes/1/export", AdminExpensive, true},
		{"admin tasks", http.MethodGet, "/api/v1/admin/operations/tasks", AdminRead, true},
		{"admin task create", http.MethodPost, "/api/v1/admin/operations/tasks", AdminExpensive, true},
		{"admin network history", http.MethodGet, "/api/v1/network/tasks/1/history", AdminRead, true},
		{"static asset", http.MethodGet, "/assets/index.js", 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			class, ok := Classify(testCase.method, testCase.path)
			if expected, actual := testCase.ok, ok; expected != actual {
				t.Fatalf("expected declared=%v, actual=%v", expected, actual)
			}
			if expected, actual := testCase.class, class; expected != actual {
				t.Fatalf("expected class %s, actual %s", expected, actual)
			}
		})
	}
}

func TestDefaultCapacityValidates(t *testing.T) {
	if err := DefaultCapacity().Validate(); err != nil {
		t.Fatalf("default capacity must validate: %v", err)
	}
}

func TestCapacityRejectsUnsafeOverrides(t *testing.T) {
	cases := map[string]func(*Capacity){
		"zero global rate":       func(capacity *Capacity) { setPolicyRate(capacity, PublicSnapshot, 0) },
		"identity above global":  func(capacity *Capacity) { setIdentityRate(capacity, PublicSnapshot, 100000) },
		"in-flight above global": func(capacity *Capacity) { setIdentityInFlight(capacity, PublicSnapshot, 4096) },
		"entries too small":      func(capacity *Capacity) { capacity.IdentityEntries = 1 },
		"entries too large":      func(capacity *Capacity) { capacity.IdentityEntries = maximumIdentityEntries + 1 },
		"ttl too small":          func(capacity *Capacity) { capacity.IdentityTTL = time.Millisecond },
		"ttl too large":          func(capacity *Capacity) { capacity.IdentityTTL = 24 * time.Hour },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			capacity := DefaultCapacity()
			mutate(&capacity)
			if err := capacity.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func setPolicyRate(capacity *Capacity, class Class, rate float64) {
	policy := capacity.Policies[class]
	policy.GlobalRate.PerSecond = rate
	capacity.Policies[class] = policy
}

func setIdentityRate(capacity *Capacity, class Class, rate float64) {
	policy := capacity.Policies[class]
	policy.IdentityRate.PerSecond = rate
	capacity.Policies[class] = policy
}

func setIdentityInFlight(capacity *Capacity, class Class, value int) {
	policy := capacity.Policies[class]
	policy.IdentityInFlight = value
	capacity.Policies[class] = policy
}

func TestFromEnvironmentAppliesValidatedOverrides(t *testing.T) {
	values := map[string]string{
		"BEAT_INGRESS_AUTH_KDF_IDENTITY_RATE":  "2",
		"BEAT_INGRESS_AUTH_KDF_IDENTITY_BURST": "3",
		"BEAT_INGRESS_IDENTITY_ENTRIES":        "64",
		"BEAT_INGRESS_IDENTITY_TTL":            "2m",
	}
	capacity, err := FromEnvironment(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("expected valid overrides: %v", err)
	}
	if expected, actual := 2.0, capacity.Policies[AuthKDF].IdentityRate.PerSecond; expected != actual {
		t.Fatalf("expected identity rate %v, actual %v", expected, actual)
	}
	if expected, actual := 64, capacity.IdentityEntries; expected != actual {
		t.Fatalf("expected entries %d, actual %d", expected, actual)
	}

	invalid := map[string]string{"BEAT_INGRESS_AUTH_KDF_IDENTITY_RATE": "-1"}
	if _, err := FromEnvironment(func(key string) string { return invalid[key] }); err == nil {
		t.Fatal("negative rate must fail startup")
	}
	invalid = map[string]string{"BEAT_INGRESS_IDENTITY_TTL": "soon"}
	if _, err := FromEnvironment(func(key string) string { return invalid[key] }); err == nil {
		t.Fatal("invalid duration must fail startup")
	}
}

func TestAdmitRejectsRateAndBusyStates(t *testing.T) {
	capacity := DefaultCapacity()
	policy := capacity.Policies[AuthKDF]
	policy.GlobalRate = Rate{PerSecond: 1, Burst: 2}
	policy.IdentityRate = Rate{PerSecond: 1, Burst: 1}
	policy.GlobalInFlight = 2
	policy.IdentityInFlight = 1
	capacity.Policies[AuthKDF] = policy
	clock := &fakeClock{now: time.Now()}
	limiter := newTestLimiter(t, capacity, clock)

	first, rejection := limiter.Admit(AuthKDF, "ip:1")
	if rejection != nil {
		t.Fatalf("first admit must pass: %v", rejection)
	}
	_, rejection = limiter.Admit(AuthKDF, "ip:1")
	if rejection == nil || rejection.Reason != ReasonIdentityBusy {
		t.Fatalf("expected identity_busy, actual %v", rejection)
	}
	if expected, actual := http.StatusTooManyRequests, rejection.Status; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	if expected, actual := 1, rejection.RetryAfter; expected != actual {
		t.Fatalf("expected retry-after %d, actual %d", expected, actual)
	}

	first.Release()
	_, rejection = limiter.Admit(AuthKDF, "ip:1")
	if rejection == nil || rejection.Reason != ReasonIdentityRate {
		t.Fatalf("expected identity_rate, actual %v", rejection)
	}
	if rejection.RetryAfter < 1 || rejection.RetryAfter > maximumRetryAfter {
		t.Fatalf("retry-after out of bounds: %d", rejection.RetryAfter)
	}
	clock.advance(time.Second)
	third, rejection := limiter.Admit(AuthKDF, "ip:1")
	if rejection != nil {
		t.Fatalf("refilled bucket must allow the request: %v", rejection)
	}
	third.Release()
}

func TestAdmitFailsClosedWhenGlobalInFlightIsExhausted(t *testing.T) {
	capacity := DefaultCapacity()
	policy := capacity.Policies[AgentWrite]
	policy.IdentityInFlight = 1
	policy.GlobalInFlight = 1
	capacity.Policies[AgentWrite] = policy
	clock := &fakeClock{now: time.Now()}
	limiter := newTestLimiter(t, capacity, clock)

	lease, rejection := limiter.Admit(AgentWrite, "node:1")
	if rejection != nil {
		t.Fatalf("first admit must pass: %v", rejection)
	}
	_, rejection = limiter.Admit(AgentWrite, "node:2")
	if rejection == nil || rejection.Reason != ReasonClassBusy {
		t.Fatalf("expected class_busy, actual %v", rejection)
	}
	if expected, actual := http.StatusServiceUnavailable, rejection.Status; expected != actual {
		t.Fatalf("expected status %d, actual %d", expected, actual)
	}
	lease.Release()
	if _, rejection := limiter.Admit(AgentWrite, "node:2"); rejection != nil {
		t.Fatalf("released lease must free the global budget: %v", rejection)
	}
}

func TestAdmitFailsClosedWhenIdentityTableIsFull(t *testing.T) {
	capacity := DefaultCapacity()
	capacity.IdentityEntries = minimumIdentityEntries
	clock := &fakeClock{now: time.Now()}
	limiter := newTestLimiter(t, capacity, clock)

	for index := 0; index < minimumIdentityEntries; index++ {
		lease, rejection := limiter.Admit(PublicSnapshot, "ip:"+string(rune('a'+index)))
		if rejection != nil {
			t.Fatalf("identity %d must be admitted: %v", index, rejection)
		}
		lease.Release()
	}
	_, rejection := limiter.Admit(PublicSnapshot, "ip:overflow")
	if rejection == nil || rejection.Reason != ReasonTableFull {
		t.Fatalf("expected table_full, actual %v", rejection)
	}

	clock.advance(capacity.IdentityTTL + time.Second)
	if _, rejection := limiter.Admit(PublicSnapshot, "ip:overflow"); rejection != nil {
		t.Fatalf("expired entries must be reused: %v", rejection)
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	limiter := newTestLimiter(t, DefaultCapacity(), clock)
	lease, rejection := limiter.Admit(AdminWrite, "session:1")
	if rejection != nil {
		t.Fatalf("admit must pass: %v", rejection)
	}
	lease.Release()
	lease.Release()
	snapshot := limiter.Snapshot()
	for _, class := range snapshot.Classes {
		if class.Class == AdminWrite.String() && class.InFlight != 0 {
			t.Fatalf("expected zero in-flight, actual %d", class.InFlight)
		}
	}
}

func TestSnapshotReportsCountersAndLimits(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	limiter := newTestLimiter(t, DefaultCapacity(), clock)
	lease, rejection := limiter.Admit(PublicSnapshot, "ip:1")
	if rejection != nil {
		t.Fatalf("admit must pass: %v", rejection)
	}
	lease.Release()
	if _, rejection := limiter.Admit(PublicHistory, "ip:1"); rejection != nil {
		t.Fatalf("expected successful admit: %v", rejection)
	}

	snapshot := limiter.Snapshot()
	if expected, actual := int(classCount), len(snapshot.Classes); expected != actual {
		t.Fatalf("expected %d classes, actual %d", expected, actual)
	}
	found := false
	for _, class := range snapshot.Classes {
		if class.Class != PublicSnapshot.String() {
			continue
		}
		found = true
		if class.Admitted != 1 {
			t.Fatalf("expected one admitted request, actual %d", class.Admitted)
		}
		if class.Limits.GlobalBurst != DefaultCapacity().Policies[PublicSnapshot].GlobalRate.Burst {
			t.Fatal("limits must reflect the effective capacity")
		}
	}
	if !found {
		t.Fatal("public snapshot class missing from snapshot")
	}
}

func TestRejectionIsAnError(t *testing.T) {
	var rejection error = newRejection(ReasonClassRate, http.StatusTooManyRequests, 0)
	if !errors.Is(rejection, rejection) {
		t.Fatal("rejection must implement error")
	}
	if expected, actual := 1, newRejection(ReasonClassRate, 429, 0).RetryAfter; expected != actual {
		t.Fatalf("expected clamp to %d, actual %d", expected, actual)
	}
	if expected, actual := maximumRetryAfter, newRejection(ReasonClassRate, 429, 9999).RetryAfter; expected != actual {
		t.Fatalf("expected clamp to %d, actual %d", expected, actual)
	}
	if expected, actual := "class_rate", ReasonClassRate.String(); expected != actual {
		t.Fatalf("expected %s, actual %s", expected, actual)
	}
	if expected, actual := "unknown", Reason(99).String(); expected != actual {
		t.Fatalf("expected %s, actual %s", expected, actual)
	}
}
