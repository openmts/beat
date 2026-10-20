package model

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPublicInteractionEventsAndSurfacesAreStableCopies(t *testing.T) {
	events := PublicInteractionEvents()
	wantEvents := []string{
		PublicInteractionPageView, PublicInteractionNodeOpen, PublicInteractionRangeChange, PublicInteractionWallboardOpen,
	}
	if expected, actual := wantEvents, events; !slices.Equal(expected, actual) {
		t.Fatalf("PublicInteractionEvents() = %v, want %v", actual, expected)
	}
	surfaces := PublicInteractionSurfaces()
	wantSurfaces := []string{PublicSurfaceDashboard, PublicSurfaceNode, PublicSurfaceWallboard, PublicSurfaceSettings}
	if expected, actual := wantSurfaces, surfaces; !slices.Equal(expected, actual) {
		t.Fatalf("PublicInteractionSurfaces() = %v, want %v", actual, expected)
	}
	events[0] = "mutated"
	if PublicInteractionEvents()[0] != PublicInteractionPageView {
		t.Fatal("PublicInteractionEvents() must return a copy of the fixed vocabulary")
	}
}

func TestPublicInteractionValidateRejectsUnknownValues(t *testing.T) {
	cases := map[string]PublicInteraction{
		"unknown event":   {Event: "click", Surface: PublicSurfaceDashboard},
		"empty event":     {Surface: PublicSurfaceDashboard},
		"unknown surface": {Event: PublicInteractionPageView, Surface: "node/42"},
		"empty surface":   {Event: PublicInteractionPageView},
	}
	for name, interaction := range cases {
		t.Run(name, func(t *testing.T) {
			if err := interaction.Validate(); !errors.Is(err, ErrPublicInteractionInvalid) {
				t.Fatalf("Validate() error = %v, want %v", err, ErrPublicInteractionInvalid)
			}
		})
	}
	valid := PublicInteraction{Event: PublicInteractionNodeOpen, Surface: PublicSurfaceNode}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestPublicTelemetryPolicyNormalizeDeduplicatesAndSorts(t *testing.T) {
	policy := PublicTelemetryPolicy{
		AllowedEvents: []string{PublicInteractionNodeOpen, " page_view ", PublicInteractionNodeOpen, "", "   "},
		UpdatedBy:     "  owner  ",
	}
	policy.Normalize()
	want := []string{PublicInteractionNodeOpen, PublicInteractionPageView}
	if expected, actual := want, policy.AllowedEvents; !slices.Equal(expected, actual) {
		t.Fatalf("Normalize() allowed events = %v, want %v", actual, expected)
	}
	if policy.SampleCeiling != MinimumPublicTelemetrySampleCeiling || policy.RetentionDays != MinimumPublicTelemetryRetentionDays {
		t.Fatalf("Normalize() bounds = %d/%d, want %d/%d", policy.SampleCeiling, policy.RetentionDays,
			MinimumPublicTelemetrySampleCeiling, MinimumPublicTelemetryRetentionDays)
	}
	if expected, actual := "owner", policy.UpdatedBy; expected != actual {
		t.Fatalf("Normalize() updated by = %q, want %q", actual, expected)
	}
}

func TestPublicTelemetryPolicyNormalizeKeepsConfiguredBounds(t *testing.T) {
	policy := PublicTelemetryPolicy{AllowedEvents: PublicInteractionEvents(), SampleCeiling: 2500, RetentionDays: 90}
	policy.Normalize()
	if policy.SampleCeiling != 2500 || policy.RetentionDays != 90 {
		t.Fatalf("Normalize() bounds = %d/%d, want 2500/90", policy.SampleCeiling, policy.RetentionDays)
	}
	want := PublicInteractionEvents()
	slices.Sort(want)
	if expected, actual := want, policy.AllowedEvents; !slices.Equal(expected, actual) {
		t.Fatalf("Normalize() allowed events = %v, want %v", actual, expected)
	}
}

func TestPublicTelemetryPolicyValidateRejectsOutOfRangeValues(t *testing.T) {
	base := DefaultPublicTelemetryPolicy()
	cases := map[string]func(*PublicTelemetryPolicy){
		"no events":            func(p *PublicTelemetryPolicy) { p.AllowedEvents = nil },
		"unregistered event":   func(p *PublicTelemetryPolicy) { p.AllowedEvents = []string{"click"} },
		"sample ceiling low":   func(p *PublicTelemetryPolicy) { p.SampleCeiling = MinimumPublicTelemetrySampleCeiling - 1 },
		"sample ceiling high":  func(p *PublicTelemetryPolicy) { p.SampleCeiling = MaximumPublicTelemetrySampleCeiling + 1 },
		"retention low":        func(p *PublicTelemetryPolicy) { p.RetentionDays = MinimumPublicTelemetryRetentionDays - 1 },
		"retention high":       func(p *PublicTelemetryPolicy) { p.RetentionDays = MaximumPublicTelemetryRetentionDays + 1 },
		"revision missing":     func(p *PublicTelemetryPolicy) { p.Revision = 0 },
		"updated by too long":  func(p *PublicTelemetryPolicy) { p.UpdatedBy = strings.Repeat("a", 65) },
		"one invalid in a set": func(p *PublicTelemetryPolicy) { p.AllowedEvents = []string{PublicInteractionPageView, "unknown"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := base
			mutate(&policy)
			if err := policy.Validate(); !errors.Is(err, ErrPublicTelemetryPolicy) {
				t.Fatalf("Validate() error = %v, want %v", err, ErrPublicTelemetryPolicy)
			}
		})
	}
	valid := DefaultPublicTelemetryPolicy()
	valid.UpdatedAt = time.Now().UTC()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestPublicTelemetryPolicyDefaultIsDisabledAndAllowsNothing(t *testing.T) {
	policy := DefaultPublicTelemetryPolicy()
	if policy.Enabled {
		t.Fatal("default policy must stay disabled until the owner enables it")
	}
	for _, event := range PublicInteractionEvents() {
		if policy.Allows(event) {
			t.Fatalf("Allows(%q) = true, want false while disabled", event)
		}
	}
	policy.Enabled = true
	if !policy.Allows(PublicInteractionPageView) {
		t.Fatal("enabled policy must allow a registered event")
	}
	if policy.Allows("click") {
		t.Fatal("enabled policy must reject an unregistered event")
	}
	policy.AllowedEvents = []string{PublicInteractionNodeOpen}
	if policy.Allows(PublicInteractionPageView) {
		t.Fatal("Allows() must honour the configured event subset")
	}
}

func TestPublicRejectionReasonsAreBounded(t *testing.T) {
	reasons := PublicRejectionReasons()
	want := []string{
		PublicRejectionDisabled, PublicRejectionBody, PublicRejectionContentType, PublicRejectionEvent,
		PublicRejectionSurface, PublicRejectionRateLimited, PublicRejectionUnauthorized,
	}
	if expected, actual := want, reasons; !slices.Equal(expected, actual) {
		t.Fatalf("PublicRejectionReasons() = %v, want %v", actual, expected)
	}
	reasons[0] = "mutated"
	if PublicRejectionReasons()[0] != PublicRejectionDisabled {
		t.Fatal("PublicRejectionReasons() must return a copy of the fixed vocabulary")
	}
	for _, reason := range want {
		if !ValidPublicRejectionReason(reason) {
			t.Fatalf("ValidPublicRejectionReason(%q) = false, want true", reason)
		}
	}
	if ValidPublicRejectionReason("error") {
		t.Fatal("ValidPublicRejectionReason must reject unregistered reasons")
	}
}

func TestPublicInteractionMetricNames(t *testing.T) {
	want := []string{PublicInteractionTotalMetric, PublicInteractionRejectedMetric}
	if expected, actual := want, PublicInteractionMetrics(); !slices.Equal(expected, actual) {
		t.Fatalf("PublicInteractionMetrics() = %v, want %v", actual, expected)
	}
	for _, key := range want {
		if !IsPublicInteractionMetric(key) {
			t.Fatalf("IsPublicInteractionMetric(%q) = false, want true", key)
		}
	}
	if IsPublicInteractionMetric("cpu") {
		t.Fatal("IsPublicInteractionMetric must reject unrelated keys")
	}
}
