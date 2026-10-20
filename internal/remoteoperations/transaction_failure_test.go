package remoteoperations

import (
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

func TestRolledBackTransactionCannotReadOrCommitOperations(t *testing.T) {
	f, _ := fileFixture(t)
	err := f.service.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		db, err := f.database.DB.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		if err := db.Rollback(); err != nil {
			return err
		}
		tx := f.service.transaction(t.Context(), db, access)
		for name, action := range closedTransactionActions(tx, f) {
			t.Run(name, func(t *testing.T) {
				if err := action(); err == nil {
					t.Fatal("closed transaction accepted an operation")
				}
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func closedTransactionActions(tx transaction, f fixture) map[string]func() error {
	item := artifact{id: "artifact", state: "uploading", direction: "deploy", size: 1, digest: fileDigest([]byte("x"))}
	creation := &taskCreation{id: "task", actor: f.actor, input: f.input("fixture"), encoded: []byte("{}"), policy: f.policy, nodes: []targetNode{{id: f.nodeID, name: "fixture"}}}
	stored := storedAttempt{attempt: Attempt{ID: "attempt", NodeID: f.nodeID}, targetID: "target", taskID: "task"}
	return map[string]func() error{
		"save policy": func() error {
			return tx.savePolicy(f.actor, PolicyInput{ID: f.policyID, Name: "Policy", Policy: f.policy})
		},
		"save snippet": func() error {
			_, err := tx.saveSnippet(f.actor, SnippetInput{ID: "snippet", Name: "Snippet", Visibility: "private", ShellFamily: "sh", Content: []byte("x")})
			return err
		},
		"prepare transfer": func() error {
			return tx.prepareTransfer(&transferCreation{input: transferInput(f, "deploy", []byte("x"))}, f.service.spool)
		},
		"create transfer task": func() error { _, err := tx.createTransferTask(f.actor, "transfer"); return err },
		"retry target": func() error {
			return tx.retryTarget(f.actor, RetryInput{TargetRequest: TargetRequest{TaskID: "task", TargetID: "target"}, Reason: "checked"})
		},
		"validate retry": func() error { return tx.validateRetry(&dispatch{targetID: "target"}, f.policy) },
		"file dispatch":  func() error { return tx.fileDispatchAllowed("task") },

		"authorize":           func() error { return tx.authorize(f.actor, false, true) },
		"node":                func() error { _, err := tx.node(NodeProof{Token: f.token}); return err },
		"capability":          func() error { _, _, err := tx.capability(f.nodeID); return err },
		"policy":              func() error { _, err := tx.policy(f.policyID); return err },
		"task quota":          func() error { return tx.activeTaskQuota(f.policyID, 5) },
		"scope":               func() error { return tx.scope(f.policyID, f.nodeID) },
		"selection":           func() error { return tx.validateSelections(f.input("selection")) },
		"task dedupe":         func() error { _, err := tx.existingTask(creation); return err },
		"persist task":        func() error { return tx.persistTask(creation) },
		"persist result":      func() error { return tx.persistResult(stored, Result{State: "succeeded"}) },
		"duplicate result":    func() error { return tx.sameResult(stored, Result{State: "succeeded"}) },
		"running policy":      func() error { _, err := tx.runningPolicy(stored); return err },
		"reflect transfer":    func() error { return tx.reflectTransferState(stored) },
		"completion transfer": func() error { return tx.validateTransferCompletion(stored, Result{State: "succeeded"}) },
		"snippet read":        func() error { _, err := tx.listSnippets(f.actor); return err },
		"policy read":         func() error { _, err := tx.listPolicies(); return err },
		"targets read":        func() error { _, err := tx.taskTargets("task"); return err },
		"target output":       func() error { _, err := tx.targetOutput(TargetRequest{TaskID: "task", TargetID: "target"}); return err },
		"transfer list":       func() error { _, err := tx.listTransfers(); return err },
		"transfer summary":    func() error { _, err := tx.transferSummary("transfer"); return err },
		"transfer targets":    func() error { _, err := tx.transferTargets("transfer"); return err },
		"transfer summaries":  func() error { _, err := tx.transferTargetSummaries("transfer"); return err },
		"transfer payload":    func() error { _, err := tx.transferTaskInput("transfer"); return err },
		"missing chunks":      func() error { _, err := tx.missingChunks("transfer"); return err },
		"artifact":            func() error { _, err := tx.artifact("transfer"); return err },
		"chunk read":          func() error { _, err := tx.readChunk(f.service.spool, item, 0); return err },
		"chunk write": func() error {
			_, err := tx.uploadChunk(f.service.spool, item, ChunkInput{Index: 0, Content: []byte("x"), Digest: fileDigest([]byte("x"))})
			return err
		},
		"spool check":        func() error { _, err := tx.spoolMissing(f.service.spool, "artifact"); return err },
		"spool expiry":       func() error { return tx.expireArtifact(f.service.spool, "artifact") },
		"scan recovery":      func() error { return tx.recoverScan(f.service, "artifact") },
		"scan ownership":     func() error { return tx.ownsScan(artifactScan{id: "scan", item: item}) },
		"artifact retention": func() error { _, err := tx.artifactRetention("transfer"); return err },
		"task expiry":        func() error { _, err := tx.expireTask("task"); return err },
		"retention":          func() error { return tx.retainOperations() },
		"task purge":         func() error { return tx.deleteRetainedTask("task") },
		"transfer purge":     func() error { return tx.retainTransfers() },
		"transfer stop":      func() error { _, err := tx.stopTransfer(f.actor, "transfer", "canceled"); return err },
		"node detach":        func() error { return tx.forceDetachNode(f.actor, ForceNodeDelete{NodeID: f.nodeID, Reason: "fixture"}) },
	}
}
