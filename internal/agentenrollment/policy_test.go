package agentenrollment

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func validAutomaticPolicy(now time.Time) GrantPolicy {
	return GrantPolicy{
		Name: "edge rollout", Mode: ApprovalAutomatic, MaximumEnrollments: 3, DefaultGroupID: "group-default",
		NodeNamePrefix: "edge", Tags: []string{"discovered"}, Remarks: "auto enrolled", Public: true,
		SSHPort: 2222, UseSourceIPAsHost: true, SourceCIDRs: []string{"203.0.113.0/24", "2001:db8::/32"},
		HostnamePattern: "edge-[0-9]+\\.example\\.com", ClaimRecovery: true, Enabled: true,
		ExpiresAt: now.Add(24 * time.Hour),
	}
}

func TestGrantPolicyAcceptsBoundedManualAndAutomaticGrants(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	if err := validAutomaticPolicy(now).Validate(now); err != nil {
		t.Fatalf("automatic policy rejected: %v", err)
	}
	manual := validAutomaticPolicy(now)
	manual.Mode, manual.NodeNamePrefix, manual.SourceCIDRs, manual.HostnamePattern = ApprovalManual, "", nil, ""
	if err := manual.Validate(now); err != nil {
		t.Fatalf("manual policy rejected: %v", err)
	}
}

func TestGrantPolicyRejectsUnboundedValues(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	cases := map[string]func(*GrantPolicy){
		"blank name":               func(p *GrantPolicy) { p.Name = "  " },
		"unknown mode":             func(p *GrantPolicy) { p.Mode = "sometimes" },
		"automatic without prefix": func(p *GrantPolicy) { p.NodeNamePrefix = "" },
		"missing group":            func(p *GrantPolicy) { p.DefaultGroupID = "" },
		"zero capacity":            func(p *GrantPolicy) { p.MaximumEnrollments = 0 },
		"huge capacity":            func(p *GrantPolicy) { p.MaximumEnrollments = MaximumGrantEnrollments + 1 },
		"past expiry":              func(p *GrantPolicy) { p.ExpiresAt = now.Add(-time.Minute) },
		"far expiry":               func(p *GrantPolicy) { p.ExpiresAt = now.Add(maximumGrantLifetime + time.Hour) },
		"bad port":                 func(p *GrantPolicy) { p.SSHPort = 0 },
		"too many tags":            func(p *GrantPolicy) { p.Tags = make([]string, 13) },
		"long remarks":             func(p *GrantPolicy) { p.Remarks = strings.Repeat("x", 2001) },
		"bad cidr":                 func(p *GrantPolicy) { p.SourceCIDRs = []string{"203.0.113.0/33"} },
		"mapped cidr":              func(p *GrantPolicy) { p.SourceCIDRs = []string{"::ffff:203.0.113.0/120"} },
		"too many cidrs":           func(p *GrantPolicy) { p.SourceCIDRs = make([]string, MaxGrantSourceCIDRs+1) },
		"bad hostname pattern":     func(p *GrantPolicy) { p.HostnamePattern = "edge(" },
	}
	for name, mutate := range cases {
		policy := validAutomaticPolicy(now)
		mutate(&policy)
		if err := policy.Validate(now); !errors.Is(err, ErrInvalidGrantPolicy) {
			t.Fatalf("%s: expected policy rejection, got %v", name, err)
		}
	}
}

func TestSourcePolicyMatchesNormalizedAddressesAndHostnames(t *testing.T) {
	policy, err := CompileSourcePolicy([]string{"203.0.113.0/24", "2001:db8::/32"}, "edge-[0-9]+\\.example\\.com")
	if err != nil {
		t.Fatal(err)
	}
	if !policy.AllowsSource(netip.MustParseAddr("203.0.113.9")) || !policy.AllowsSource(netip.MustParseAddr("2001:db8::5")) {
		t.Fatal("trusted sources were rejected")
	}
	if policy.AllowsSource(netip.MustParseAddr("198.51.100.9")) || policy.AllowsSource(netip.MustParseAddr("2001:db9::5")) {
		t.Fatal("untrusted source was accepted")
	}
	if !policy.AllowsHostname("edge-7.example.com") || policy.AllowsHostname("edge-7.example.com.evil") {
		t.Fatal("hostname anchoring is not enforced")
	}
	unrestricted, err := CompileSourcePolicy(nil, "")
	if err != nil || !unrestricted.Unrestricted() || !unrestricted.AllowsHostname("anything") {
		t.Fatalf("unrestricted policy: %v", err)
	}
}

func TestDeriveNodeNameSanitizesInsteadOfAppendingSuffixes(t *testing.T) {
	name, err := DeriveNodeName("Edge Fleet", "Edge-01.Example.Com")
	if err != nil || name != "edge-fleet-edge-01-example-com" {
		t.Fatalf("derived %q (%v)", name, err)
	}
	if _, err := DeriveNodeName("", "  ..  "); !errors.Is(err, ErrInvalidGrantPolicy) {
		t.Fatalf("empty hostname accepted: %v", err)
	}
	long, err := DeriveNodeName("prod", strings.Repeat("host.", 40))
	if err != nil || len(long) > MaxDerivedNodeNameLength || strings.HasSuffix(long, "-") {
		t.Fatalf("long name %q (%v)", long, err)
	}
	if SanitizeNodeNamePart("Node #1") != "node-1" {
		t.Fatal("preview sanitization diverged from derivation")
	}
}

func TestRequestAndCredentialStateMachines(t *testing.T) {
	if !RequestPending.CanApprove() || RequestApproved.CanApprove() || !RequestApproved.CanClaim() {
		t.Fatal("approval transitions are wrong")
	}
	if RequestClaimed.CanClaim() || !RequestClaimed.Terminal() || !RequestExpired.Terminal() ||
		!RequestRejected.Terminal() || RequestPending.Terminal() || RequestApproved.Terminal() {
		t.Fatal("terminal transitions are wrong")
	}
	if !RequestPending.CanReject() || !RequestApproved.CanReject() || RequestClaimed.CanReject() || !RequestRejected.Valid() {
		t.Fatal("rejection transitions are wrong")
	}
	if RequestStatus("unknown").Valid() || RequestStatus("unknown").Terminal() {
		t.Fatal("unknown status was treated as valid")
	}
	if !CredentialActive.CanAuthenticateAgent() || CredentialPendingClaim.CanAuthenticateAgent() ||
		CredentialLegacy.CanAuthenticateAgent() || CredentialRevoked.CanAuthenticateAgent() {
		t.Fatal("credential states must only authenticate when active")
	}
	if !CredentialPendingClaim.Hidden() || CredentialActive.Hidden() {
		t.Fatal("only pending claims are hidden")
	}
	if DeriveCredentialState(false, false) != CredentialLegacy || DeriveCredentialState(true, false) != CredentialActive ||
		DeriveCredentialState(true, true) != CredentialRevoked {
		t.Fatal("credential derivation diverged from the migration contract")
	}
}
