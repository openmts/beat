package remoteoperations

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMetadataReadsAndOutputRequireCurrentAuthority(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "read")
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded", Stdout: []byte("private-result")}}); err != nil {
		t.Fatal(err)
	}
	tasks, err := f.service.ListTasks(t.Context(), f.actor)
	if err != nil || len(tasks) != 1 {
		t.Fatal(tasks, err)
	}
	task, err := f.service.GetTask(t.Context(), f.actor, id)
	if err != nil || len(task.Targets) != 1 || task.Targets[0].State != "succeeded" {
		t.Fatal(task, err)
	}
	encoded, err := json.Marshal(task)
	if err != nil || bytes.Contains(encoded, []byte("private")) {
		t.Fatal("metadata leaked command or result")
	}
	output, err := f.service.TargetOutput(t.Context(), f.actor, TargetRequest{id, task.Targets[0].ID})
	if err != nil || string(output.Stdout) != "private-result" {
		t.Fatal(output, err)
	}
	policies, err := f.service.ListPolicies(t.Context(), f.actor)
	if err != nil || len(policies) != 1 || len(policies[0].NodeIDs) != 1 {
		t.Fatal(policies, err)
	}
	if _, err := f.service.GetTask(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := f.service.TargetOutput(t.Context(), f.actor, TargetRequest{"missing", "missing"}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := f.service.ListTasks(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.GetTask(t.Context(), Actor{}, id); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.ListPolicies(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.TargetOutput(t.Context(), Actor{}, TargetRequest{}); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestUnknownRetryRequiresRiskAcknowledgementAndKeepsPriorResult(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "retry")
	first := f.lease(t)
	if err := f.service.Start(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	task, err := f.service.GetTask(t.Context(), f.actor, id)
	if err != nil {
		t.Fatal(err)
	}
	input := RetryInput{TargetRequest: TargetRequest{id, task.Targets[0].ID}, Reason: "operator checked host"}
	if f.service.RetryTarget(t.Context(), f.actor, input) != ErrConflict {
		t.Fatal("running attempt retried")
	}
	f.sql(t, "UPDATE operation_task_targets SET state='unknown'")
	f.sql(t, "UPDATE operation_attempts SET state='unknown'")
	if f.service.RetryTarget(t.Context(), f.actor, input) != ErrConflict {
		t.Fatal("unknown retry without risk acknowledgement")
	}
	input.AcknowledgeUnknownRisk = true
	if err := f.service.RetryTarget(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	second := f.lease(t)
	if second.ID == first.ID {
		t.Fatal("attempt id reused")
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: first, Result: Result{State: "interrupted"}}); err != nil {
		t.Fatal(err)
	}
	task, err = f.service.GetTask(t.Context(), f.actor, id)
	if err != nil || task.Targets[0].AttemptID != second.ID || task.Targets[0].State != "leased" {
		t.Fatal("old result changed current attempt", err)
	}
	if f.service.RetryTarget(t.Context(), f.actor, RetryInput{}) != ErrInvalid {
		t.Fatal("empty retry reason")
	}
	input.TargetID = "missing"
	if f.service.RetryTarget(t.Context(), f.actor, input) != ErrNotFound {
		t.Fatal("missing retry")
	}
	if f.service.RetryTarget(t.Context(), Actor{}, input) != ErrForbidden {
		t.Fatal("anonymous retry")
	}
}

func TestReadDependenciesFailClosed(t *testing.T) {
	for _, table := range []string{"operation_tasks", "operation_task_targets", "operation_policies"} {
		t.Run(table, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "read")
			f.sql(t, "ALTER TABLE "+table+" RENAME TO fixture_unavailable")
			switch table {
			case "operation_tasks":
				if _, err := f.service.ListTasks(t.Context(), f.actor); err != ErrUnavailable {
					t.Fatal(err)
				}
			case "operation_task_targets":
				if _, err := f.service.GetTask(t.Context(), f.actor, id); err != ErrUnavailable {
					t.Fatal(err)
				}
			case "operation_policies":
				if _, err := f.service.ListPolicies(t.Context(), f.actor); err != ErrUnavailable {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDisablePolicyAndDeleteSnippetKeepHistoricalState(t *testing.T) {
	f := testFixture(t)
	policy, err := f.service.GetPolicy(t.Context(), f.actor, f.policyID)
	if err != nil || policy.ID != f.policyID {
		t.Fatal(err)
	}
	if _, err := f.service.GetPolicy(t.Context(), Actor{}, f.policyID); err != ErrForbidden {
		t.Fatal(err)
	}
	if _, err := f.service.GetPolicy(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := f.service.DisablePolicy(t.Context(), f.actor, f.policyID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateTask(t.Context(), f.actor, f.input("disabled")); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.DisablePolicy(t.Context(), Actor{}, f.policyID); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.DisablePolicy(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	version, err := f.service.SaveSnippet(t.Context(), f.actor, SnippetInput{Name: "draft", Visibility: "private", ShellFamily: "sh", Content: []byte("retained")})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteSnippet(t.Context(), Actor{}, version.ID); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.DeleteSnippet(t.Context(), f.actor, version.ID); err != nil {
		t.Fatal(err)
	}
	list, err := f.service.ListSnippets(t.Context(), f.actor)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	if err := f.service.DeleteSnippet(t.Context(), f.actor, version.ID); err != ErrNotFound {
		t.Fatal(err)
	}
	plain, err := f.service.SnippetContent(t.Context(), f.actor, version)
	if err != ErrNotFound || len(plain) != 0 {
		t.Fatal("deleted snippet content remained readable", err)
	}
}
