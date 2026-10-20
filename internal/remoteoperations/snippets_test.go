package remoteoperations

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSnippetVersionsApprovalAndPrivateContent(t *testing.T) {
	f := testFixture(t)
	input := SnippetInput{Name: "Deploy", Visibility: "shared", ShellFamily: "sh", Content: []byte("private-command")}
	version, err := f.service.SaveSnippet(t.Context(), f.actor, input)
	if err != nil || version.Version != 1 || version.Approved {
		t.Fatal(version, err)
	}
	if err := f.service.ApproveSnippet(t.Context(), f.actor, version); err != nil {
		t.Fatal(err)
	}
	versions, err := f.service.ListSnippets(t.Context(), f.actor)
	if err != nil || len(versions) != 1 || !versions[0].Approved {
		t.Fatal(versions, err)
	}
	encoded, err := json.Marshal(versions)
	if err != nil || strings.Contains(string(encoded), "private-command") {
		t.Fatal("list leaked content")
	}
	plain, err := f.service.SnippetContent(t.Context(), f.actor, version)
	if err != nil || !bytes.Equal(input.Content, plain) {
		t.Fatal(err)
	}
	input.ID = version.ID
	input.ExpectedVersion = 1
	input.Content = []byte("edited-command")
	updated, err := f.service.SaveSnippet(t.Context(), f.actor, input)
	if err != nil || updated.Version != 2 || updated.Approved {
		t.Fatal(updated, err)
	}
	if _, err := f.service.SaveSnippet(t.Context(), f.actor, input); err != ErrConflict {
		t.Fatal(err)
	}
	plain, err = f.service.SnippetContent(t.Context(), f.actor, version)
	if err != nil || string(plain) != "private-command" {
		t.Fatal("historical content changed", err)
	}
	if err := f.service.ApproveSnippet(t.Context(), f.actor, version); err != ErrConflict {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE admin_sessions SET reauthenticated_until=NULL")
	if _, err := f.service.SnippetContent(t.Context(), f.actor, updated); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.ApproveSnippet(t.Context(), f.actor, updated); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestSnippetExecutionPinsExactApprovedContent(t *testing.T) {
	f := testFixture(t)
	f.policy.Kinds = []string{"snippet"}
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 1, Name: "Snippets", Policy: f.policy, NodeIDs: []string{f.nodeID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Advertise(t.Context(), NodeProof{Token: f.token}, f.policy); err != nil {
		t.Fatal(err)
	}
	version, err := f.service.SaveSnippet(t.Context(), f.actor, SnippetInput{Name: "Reviewed", Visibility: "shared", ShellFamily: "sh", Content: []byte("printf private-output")})
	if err != nil {
		t.Fatal(err)
	}
	input := f.input("snippet")
	input.Limits.Kind = "snippet"
	input.SnippetID = version.ID
	input.SnippetVersion = version.Version
	if _, err := f.service.CreateTask(t.Context(), f.actor, input); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.ApproveSnippet(t.Context(), f.actor, version); err != nil {
		t.Fatal(err)
	}
	input.Command = []byte("tampered")
	if _, err := f.service.CreateTask(t.Context(), f.actor, input); err != ErrConflict {
		t.Fatal(err)
	}
	input.Command = []byte("printf private-output")
	if _, err := f.service.CreateTask(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
}

func TestSnippetValidationAndFailures(t *testing.T) {
	f := testFixture(t)
	for _, input := range []SnippetInput{{}, {Name: "x", Content: []byte("x")}, {Name: "x", Content: []byte("x"), Visibility: "private", ShellFamily: "bad"}} {
		if _, err := f.service.SaveSnippet(t.Context(), f.actor, input); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	if _, err := f.service.ListSnippets(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.SnippetContent(t.Context(), f.actor, SnippetVersion{ID: "missing", Version: 1}); err != ErrNotFound {
		t.Fatal(err)
	}
	for _, table := range []string{"operation_snippets", "operation_snippet_versions", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			fixture := testFixture(t)
			fixture.sql(t, "CREATE TRIGGER reject_snippet BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			if _, err := fixture.service.SaveSnippet(t.Context(), fixture.actor, SnippetInput{Name: "name", Visibility: "private", ShellFamily: "sh", Content: []byte("x")}); err == nil {
				t.Fatal("storage failure accepted")
			}
		})
	}
}
