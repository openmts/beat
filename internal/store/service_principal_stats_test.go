package store

import (
	"fmt"
	"testing"
	"time"
)

func TestServicePrincipalStatisticsCountsStates(t *testing.T) {
	database := setupTestDB(t)
	principals := NewServicePrincipalStore(database.DB)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	actor := serviceActor()
	create := func(id string, expiresAt time.Time) {
		t.Helper()
		principal := servicePrincipalFixture(id)
		principal.ExpiresAt = expiresAt
		principal.TokenHash = []byte(fmt.Sprintf("%-32s", id)[:32])
		if err := principals.Create(t.Context(), principal, actor); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	create("active-far", now.Add(30*24*time.Hour))
	create("active-soon", now.Add(24*time.Hour))
	create("revoked-one", now.Add(30*24*time.Hour))
	if err := principals.Revoke(t.Context(), ServicePrincipalRevocation{
		ID: "revoked-one", Reason: "test", At: now, Actor: actor,
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	create("rotated-predecessor", now.Add(30*24*time.Hour))
	successor := servicePrincipalFixture("rotated-successor")
	successor.ExpiresAt = now.Add(30 * 24 * time.Hour)
	successor.TokenHash = []byte("fedcba9876543210fedcba9876543210")
	if err := principals.Rotate(t.Context(), ServicePrincipalRotation{
		Previous: "rotated-predecessor", Next: successor, Overlap: time.Hour, At: now, Actor: actor,
	}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	statistics, err := principals.Statistics(t.Context(), now)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	want := ServicePrincipalStatistics{Active: 4, Expiring: 1, Revoked: 1}
	if statistics != want {
		t.Fatalf("statistics = %+v, want %+v", statistics, want)
	}
}
