package remoteoperations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/secretbox"
)

type CompletionQueue interface {
	EnqueueOperation(context.Context, notificationqueue.Request, func(*sql.Tx, secretbox.ResourceAccess) error) (notificationqueue.Receipt, error)
}

var errResultConflict = errors.New("operation completion conflicts with retained result")

func (service *Service) Complete(ctx context.Context, input Completion) error {
	if service.queue == nil {
		return ErrForbidden
	}
	if input.Result.Validate(MaximumOutputBytes) != nil {
		return ErrInvalid
	}
	kind := "operation.failed"
	if input.Result.State == "succeeded" {
		kind = "operation.completed"
	}
	conflict := false
	err := service.atomicMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) {
		err := tx.complete(input)
		if errors.Is(err, errResultConflict) {
			conflict = true
			if err := tx.auditResultConflict(input.Attempt.ID); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &notificationqueue.Request{SourceKey: "operation:" + input.Attempt.ID, SourceKind: "operation", SourceID: input.Attempt.ID,
			EventKind: kind, Subject: "Remote operation completed", Text: "An operation result is available in Beat.", UseSubscriptions: true}, nil
	})
	if err == nil && conflict {
		return ErrConflict
	}
	return err
}

func (tx transaction) auditResultConflict(id string) error {
	return tx.auditEvent(operationAudit{action: "operation.result.conflict", id: id, outcome: "failure"})
}

func (tx transaction) complete(input Completion) error {
	item, err := tx.loadAttempt(input.Attempt)
	if err != nil {
		return err
	}
	proof := Proof{NodeID: item.attempt.NodeID, Nonce: input.Attempt.Nonce, Now: tx.now}
	if err := item.attempt.Authenticate(proof); err != nil {
		return err
	}
	if err := input.Result.Validate(item.outputLimit); err != nil {
		return err
	}
	if item.attempt.CompletedAt != nil {
		return tx.sameResult(item, input.Result)
	}
	if err := tx.validateTransferCompletion(item, input.Result); err != nil {
		if !item.restored || !errors.Is(err, ErrConflict) {
			return err
		}
	}
	if item.attempt.StartedAt == nil {
		if !item.restored || item.attempt.State != "unknown" {
			return ErrConflict
		}
		item.attempt.State = input.Result.State
		item.attempt.CompletedAt = &tx.now
	} else if err := item.attempt.Complete(proof, input.Result); err != nil {
		return err
	}
	if err := tx.persistResult(item, input.Result); err != nil {
		return err
	}
	if err := tx.persistAttempt(item); err != nil {
		return err
	}
	return tx.audit("", "operation.attempt.completed", item.attempt.ID)
}

func (tx transaction) persistResult(item storedAttempt, result Result) error {
	stdout, err := tx.access.Seal(result.Stdout, secretbox.OperationOutputAAD(item.attempt.ID, "stdout"))
	if err != nil {
		return ErrUnavailable
	}
	stderr, err := tx.access.Seal(result.Stderr, secretbox.OperationOutputAAD(item.attempt.ID, "stderr"))
	if err != nil {
		return ErrUnavailable
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return ErrInvalid
	}
	defer clear(encoded)
	digest, err := fingerprint(tx.access, encoded)
	if err != nil {
		return err
	}
	result.StdoutBytes = len(result.Stdout)
	result.StderrBytes = len(result.Stderr)
	result.Stdout = nil
	result.Stderr = nil
	result.StdoutSHA256 = ""
	result.StderrSHA256 = ""
	metadata, err := json.Marshal(result)
	if err != nil {
		return ErrInvalid
	}
	return tx.one("UPDATE operation_attempts SET stdout_encrypted=?,stderr_encrypted=?,result_digest=?,result_json=? WHERE id=?", stdout, stderr, digest, string(metadata), item.attempt.ID)
}

func (tx transaction) sameResult(item storedAttempt, result Result) error {
	var stdout, stderr []byte
	var metadata string
	if err := tx.tx.QueryRowContext(tx.ctx, "SELECT stdout_encrypted,stderr_encrypted,result_json FROM operation_attempts WHERE id=?", item.attempt.ID).Scan(&stdout, &stderr, &metadata); err != nil {
		return ErrUnavailable
	}
	out, err := tx.access.Open(stdout, secretbox.OperationOutputAAD(item.attempt.ID, "stdout"))
	if err != nil {
		return ErrUnavailable
	}
	defer clear(out)
	errors, err := tx.access.Open(stderr, secretbox.OperationOutputAAD(item.attempt.ID, "stderr"))
	if err != nil {
		return ErrUnavailable
	}
	defer clear(errors)
	if !bytes.Equal(out, result.Stdout) || !bytes.Equal(errors, result.Stderr) {
		return errResultConflict
	}
	result.StdoutBytes = len(result.Stdout)
	result.StderrBytes = len(result.Stderr)
	result.Stdout = nil
	result.Stderr = nil
	result.StdoutSHA256 = ""
	result.StderrSHA256 = ""
	encoded, err := json.Marshal(result)
	if err != nil || string(encoded) != metadata {
		return errResultConflict
	}
	return nil
}
