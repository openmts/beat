package serviceprincipal

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

var policyNow = time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)

func validIssue() IssueRequest {
	return IssueRequest{
		Name:        "  nightly backup  ",
		Purpose:     "nightly backup automation",
		Scopes:      []model.ServiceScope{model.ServiceScopeBackupsCreate, model.ServiceScopeNodesRead},
		Resources:   []model.ServiceResource{{Kind: model.ServiceResourceNode, ID: "node-one"}},
		Visibility:  "public",
		SourceCIDRs: []string{"192.0.2.10/24", "192.0.2.0/24"},
		ExpiresAt:   policyNow.Add(24 * time.Hour),
	}
}

func TestValidateNormalizesPolicy(t *testing.T) {
	policy, err := validIssue().Validate(policyNow)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Name != "nightly backup" || policy.Visibility != model.ServiceVisibilityPublic {
		t.Fatal("policy name or visibility was not normalized", policy.Name)
	}
	if len(policy.SourceCIDRs) != 1 || policy.SourceCIDRs[0] != "192.0.2.0/24" {
		t.Fatal("source CIDRs were not canonicalized", policy.SourceCIDRs)
	}
	if len(policy.Scopes) != 2 || policy.Scopes[0] != model.ServiceScopeBackupsCreate {
		t.Fatal("scopes were not sorted", policy.Scopes)
	}
	if policy.ExpiresAt.Location() != time.UTC {
		t.Fatal("expiry was not normalized to UTC")
	}
}

func TestValidateDefaultsVisibilityToPrivate(t *testing.T) {
	request := validIssue()
	request.Visibility = ""
	policy, err := request.Validate(policyNow)
	if err != nil || policy.Visibility != model.ServiceVisibilityPrivate {
		t.Fatal("empty visibility did not default to private", policy.Visibility, err)
	}
}

func TestValidateRejectsIncompleteIdentity(t *testing.T) {
	cases := map[string]func(*IssueRequest){
		"blank name":    func(r *IssueRequest) { r.Name = "   " },
		"long name":     func(r *IssueRequest) { r.Name = strings.Repeat("a", model.MaximumServiceTokenName+1) },
		"invalid name":  func(r *IssueRequest) { r.Name = "bad\x00name" },
		"long purpose":  func(r *IssueRequest) { r.Purpose = strings.Repeat("a", model.MaximumServiceTokenPurpose+1) },
		"empty scopes":  func(r *IssueRequest) { r.Scopes = nil },
		"unknown scope": func(r *IssueRequest) { r.Scopes = []model.ServiceScope{"nodes:delete"} },
	}
	for name, mutate := range cases {
		request := validIssue()
		mutate(&request)
		if _, err := request.Validate(policyNow); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestValidateRejectsResourceAndSourceViolations(t *testing.T) {
	cases := map[string]struct {
		mutate func(*IssueRequest)
		target error
	}{
		"unknown kind": {func(r *IssueRequest) { r.Resources = []model.ServiceResource{{Kind: "cluster", ID: "one"}} }, ErrResource},
		"empty id":     {func(r *IssueRequest) { r.Resources = []model.ServiceResource{{Kind: model.ServiceResourceNode}} }, ErrResource},
		"control id": {func(r *IssueRequest) {
			r.Resources = []model.ServiceResource{{Kind: model.ServiceResourceNode, ID: "bad\nid"}}
		}, ErrResource},
		"kind not scoped": {func(r *IssueRequest) {
			r.Resources = []model.ServiceResource{{Kind: model.ServiceResourceChannel, ID: "channel-one"}}
		}, ErrResource},
		"too many":       {func(r *IssueRequest) { r.Resources = manyResources(model.MaximumServiceTokenResources + 1) }, ErrResource},
		"bad visibility": {func(r *IssueRequest) { r.Visibility = "internal" }, ErrResource},
		"bad cidr":       {func(r *IssueRequest) { r.SourceCIDRs = []string{"192.0.2.0/33"} }, ErrSource},
		"too many cidrs": {func(r *IssueRequest) { r.SourceCIDRs = manyCIDRs(model.MaximumServiceTokenCIDRs + 1) }, ErrSource},
	}
	for name, test := range cases {
		request := validIssue()
		test.mutate(&request)
		if _, err := request.Validate(policyNow); !errors.Is(err, test.target) {
			t.Fatalf("%s returned %v, want %v", name, err, test.target)
		}
	}
}

func TestValidateBoundsExpiry(t *testing.T) {
	cases := map[string]struct {
		mutate func(*IssueRequest)
		target error
	}{
		"zero":           {func(r *IssueRequest) { r.ExpiresAt = time.Time{} }, ErrExpiry},
		"past":           {func(r *IssueRequest) { r.ExpiresAt = policyNow.Add(-time.Minute) }, ErrExpiry},
		"beyond default": {func(r *IssueRequest) { r.ExpiresAt = policyNow.Add(model.DefaultServiceTokenLifetime + time.Hour) }, ErrExpiry},
		"beyond extended": {func(r *IssueRequest) {
			r.ExpiresAt = policyNow.Add(model.ExtendedServiceTokenLifetime + time.Hour)
			r.Extended = true
		}, ErrExpiry},
	}
	for name, test := range cases {
		request := validIssue()
		test.mutate(&request)
		if _, err := request.Validate(policyNow); !errors.Is(err, test.target) {
			t.Fatalf("%s returned %v, want %v", name, err, test.target)
		}
	}
	extended := validIssue()
	extended.ExpiresAt = policyNow.Add(model.ExtendedServiceTokenLifetime - time.Hour)
	extended.Extended = true
	if _, err := extended.Validate(policyNow); err != nil {
		t.Fatal("owner-approved extended expiry was rejected", err)
	}
}

func TestValidateDeduplicatesScopesAndResources(t *testing.T) {
	request := validIssue()
	request.Scopes = append(request.Scopes, model.ServiceScopeNodesRead)
	request.Resources = append(request.Resources, model.ServiceResource{Kind: model.ServiceResourceNode, ID: "node-one"})
	policy, err := request.Validate(policyNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Scopes) != 2 || len(policy.Resources) != 1 {
		t.Fatal("duplicates were not collapsed", policy.Scopes, policy.Resources)
	}
}

func manyResources(count int) []model.ServiceResource {
	resources := make([]model.ServiceResource, 0, count)
	for index := range count {
		resources = append(resources, model.ServiceResource{Kind: model.ServiceResourceNode, ID: fmt.Sprintf("node-%d", index)})
	}
	return resources
}

func manyCIDRs(count int) []string {
	values := make([]string, 0, count)
	for index := range count {
		values = append(values, fmt.Sprintf("192.0.%d.0/24", index))
	}
	return values
}
