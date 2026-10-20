package remoteoperations

import (
	"testing"
	"time"
)

func TestPolicyQuotaAndSnippetOwnership(t *testing.T) {
	f := testFixture(t)
	f.sql(t, `WITH RECURSIVE ids(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM ids WHERE n<199)
 INSERT INTO operation_policies(id,name,enabled,configuration_json,version,creator_id,created_at,updated_at)
 SELECT 'quota-'||n,'Quota',0,'{}',1,'owner',1,1 FROM ids`)
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{Name: "Overflow", Policy: f.policy}); err != ErrForbidden {
		t.Fatal(err)
	}
	snippet, err := f.service.SaveSnippet(t.Context(), f.actor, SnippetInput{Name: "Draft", Visibility: "private", ShellFamily: "sh", Content: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE operation_snippets SET owner_id='someone-else' WHERE id=?", snippet.ID)
	if _, err := f.service.SaveSnippet(t.Context(), f.actor, SnippetInput{ID: snippet.ID, ExpectedVersion: 1, Name: "Changed", Visibility: "private", ShellFamily: "sh", Content: []byte("x")}); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := f.service.DeleteSnippet(t.Context(), f.actor, snippet.ID); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestTaskRequestNormalizationRejectsUnsupportedFields(t *testing.T) {
	f := testFixture(t)
	for _, change := range []func(*TaskInput){func(i *TaskInput) { i.ShellFamily = "unknown" }, func(i *TaskInput) { i.File = &FileSpec{} }, func(i *TaskInput) { i.Limits.Targets = -1 }, func(i *TaskInput) { i.Limits.Targets = 201 }, func(i *TaskInput) { i.SnippetID = "not-shell" }} {
		input := f.input("invalid")
		change(&input)
		if _, err := f.service.CreateTask(t.Context(), f.actor, input); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	local := f.policy
	local.SupportedShells = []string{"powershell"}
	if _, err := f.service.Advertise(t.Context(), NodeProof{Token: f.token}, local); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateTask(t.Context(), f.actor, f.input("shell")); err != ErrForbidden {
		t.Fatal("unsupported shell queued", err)
	}
}

func TestRunningPolicyRevocationRequestsCancellation(t *testing.T) {
	f := testFixture(t)
	f.task(t, "revoke")
	attempt := f.lease(t)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	f.sql(t, "UPDATE operation_policies SET enabled=0")
	if err := f.service.Start(t.Context(), attempt); err != ErrConflict {
		t.Fatal("revoked durable authorization reused", err)
	}
	if cancel, err := f.service.Heartbeat(t.Context(), attempt); err != nil || !cancel {
		t.Fatal(cancel, err)
	}
}

func TestRunningTimeoutAndSupersededAttemptRequestStop(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "timeout")
	attempt := f.lease(t)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	now := f.service.now().Add(time.Minute)
	f.service.now = func() time.Time { return now }
	if cancel, err := f.service.Heartbeat(t.Context(), attempt); err != nil || !cancel {
		t.Fatal(cancel, err)
	}
	f.sql(t, "UPDATE operation_attempts SET state='unknown'")
	f.sql(t, "UPDATE operation_task_targets SET state='unknown'")
	task, err := f.service.GetTask(t.Context(), f.actor, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.RetryTarget(t.Context(), f.actor, RetryInput{TargetRequest: TargetRequest{TaskID: id, TargetID: task.Targets[0].ID}, Reason: "checked prior host", AcknowledgeUnknownRisk: true}); err != nil {
		t.Fatal(err)
	}
	if cancel, err := f.service.Heartbeat(t.Context(), attempt); err != nil || !cancel {
		t.Fatal("superseded process was allowed to continue", cancel, err)
	}
}
