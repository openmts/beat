package serviceprincipal

import (
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func principalFixture() model.ServicePrincipal {
	return model.ServicePrincipal{
		ExpiresAt: policyNow.Add(24 * time.Hour),
		Scopes:    []model.ServiceScope{model.ServiceScopeNodesRead},
	}
}

func TestUsableRejectsRevokedExpiredAndRotatedTokens(t *testing.T) {
	value := principalFixture()
	if !Usable(value, policyNow) {
		t.Fatal("active principal was rejected")
	}
	revoked := policyNow.Add(-time.Minute)
	value.RevokedAt = &revoked
	if Usable(value, policyNow) {
		t.Fatal("revoked principal was accepted")
	}
	value.RevokedAt = nil
	value.ExpiresAt = policyNow
	if Usable(value, policyNow) {
		t.Fatal("principal expiring exactly now was accepted")
	}
	value = principalFixture()
	superseded := policyNow.Add(-time.Hour)
	value.SupersededAt = &superseded
	value.RotationOverlapSeconds = 3600
	if Usable(value, policyNow) {
		t.Fatal("superseded principal past its overlap was accepted")
	}
	if !Usable(value, superseded.Add(30*time.Minute)) {
		t.Fatal("superseded principal inside its overlap was rejected")
	}
}

func TestAllowsSourceUsesCanonicalPrefixes(t *testing.T) {
	value := principalFixture()
	if !AllowsSource(value, netip.AddrFrom4([4]byte{198, 51, 100, 7})) {
		t.Fatal("unrestricted principal rejected a source")
	}
	value.SourceCIDRs = []string{"192.0.2.0/24", "2001:db8::/32"}
	if !AllowsSource(value, netip.AddrFrom4([4]byte{192, 0, 2, 9})) {
		t.Fatal("matching IPv4 source was rejected")
	}
	if AllowsSource(value, netip.AddrFrom4([4]byte{198, 51, 100, 7})) {
		t.Fatal("non-matching source was accepted")
	}
	if !AllowsSource(value, netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("matching IPv6 source was rejected")
	}
	if AllowsSource(value, netip.Addr{}) {
		t.Fatal("invalid source address was accepted")
	}
	value.SourceCIDRs = []string{"::ffff:192.0.2.0/120"}
	if AllowsSource(value, netip.AddrFrom4([4]byte{192, 0, 2, 9})) {
		t.Fatal("mapped prefix was trusted without unmapping")
	}
	value.SourceCIDRs = []string{"not-a-prefix"}
	if AllowsSource(value, netip.AddrFrom4([4]byte{192, 0, 2, 9})) {
		t.Fatal("unparseable prefix was trusted")
	}
}

func TestPermitsIntersectsScopeAndResources(t *testing.T) {
	value := principalFixture()
	value.Resources = []model.ServiceResource{{Kind: model.ServiceResourceNode, ID: "node-one"}}
	if !Permits(value, Request{Scope: model.ServiceScopeNodesRead}) {
		t.Fatal("scope-only request was rejected")
	}
	if !Permits(value, Request{Scope: model.ServiceScopeNodesRead, Kind: model.ServiceResourceNode, ID: "node-one"}) {
		t.Fatal("matching resource was rejected")
	}
	if Permits(value, Request{Scope: model.ServiceScopeNodesRead, Kind: model.ServiceResourceNode, ID: "node-two"}) {
		t.Fatal("unrelated resource was accepted")
	}
	if Permits(value, Request{Scope: model.ServiceScopeNodesRead, Kind: model.ServiceResourceNode}) {
		t.Fatal("unconstrained lookup among constrained resources was accepted")
	}
	if !Permits(value, Request{Scope: model.ServiceScopeNodesRead, Kind: model.ServiceResourceChannel, ID: "channel-one"}) {
		t.Fatal("a different unrestricted resource kind was rejected")
	}
	if Permits(value, Request{Scope: model.ServiceScopeBackupsCreate}) {
		t.Fatal("unregistered scope was accepted")
	}
}
