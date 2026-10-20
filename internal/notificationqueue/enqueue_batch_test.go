package notificationqueue

import (
	"database/sql"
	"errors"
	"testing"
)

func TestEnqueueBatchCommitsTwoNoticesWithOneMutationAndStableOrder(t *testing.T) {
	service, db := queueFixture(t)
	addQueueChannel(t, service, "channel")
	if _, err := db.DB.Exec("CREATE TABLE batch_source(id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	requests := []Request{enqueueRequest("health-recovered"), enqueueRequest("threshold-triggered")}
	calls := 0
	mutation := func(tx *sql.Tx) error {
		calls++
		_, err := tx.Exec("INSERT INTO batch_source VALUES('transition')")
		return err
	}
	first, err := service.EnqueueBatch(t.Context(), requests, mutation)
	if err != nil || len(first) != 2 || calls != 1 || first[0].MessageID == first[1].MessageID {
		t.Fatal("batch commit failed", err)
	}
	second, err := service.EnqueueBatch(t.Context(), requests, mutation)
	if err != nil || calls != 1 || !second[0].Duplicate || !second[1].Duplicate || second[0].MessageID != first[0].MessageID || second[1].MessageID != first[1].MessageID {
		t.Fatal("batch retry changed order or repeated mutation", err)
	}
}

func TestEnqueueBatchRollsBackSecondMessageFailureAndMarksPersistence(t *testing.T) {
	service, db := queueFixture(t)
	if _, err := db.DB.Exec("CREATE TABLE batch_source(id TEXT PRIMARY KEY); CREATE TRIGGER fail_second BEFORE INSERT ON notification_messages WHEN NEW.source_key='second' BEGIN SELECT RAISE(ABORT,'second failed'); END"); err != nil {
		t.Fatal(err)
	}
	requests := []Request{enqueueRequest("first"), enqueueRequest("second")}
	_, err := service.EnqueueBatch(t.Context(), requests, func(tx *sql.Tx) error { _, err := tx.Exec("INSERT INTO batch_source VALUES('transition')"); return err })
	if !errors.Is(err, ErrQueueUnavailable) || !service.persistenceBroken.Load() {
		t.Fatal("outbox failure was hidden", err)
	}
	for _, table := range []string{"batch_source", "notification_messages", "notification_deliveries"} {
		var count int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("batch partly escaped rollback", table, err)
		}
	}
	if _, err := db.DB.Exec("DROP TRIGGER fail_second"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EnqueueBatch(t.Context(), requests, nil); err != nil || service.persistenceBroken.Load() {
		t.Fatal("successful batch did not restore persistence health", err)
	}
}

func TestEnqueueBatchMixedExistingRequiresAuthoritativeSkip(t *testing.T) {
	service, _ := queueFixture(t)
	if _, err := service.Enqueue(t.Context(), enqueueRequest("existing"), nil); err != nil {
		t.Fatal(err)
	}
	requests := []Request{enqueueRequest("existing"), enqueueRequest("missing")}
	for _, mutation := range []func(*sql.Tx) error{nil, func(*sql.Tx) error { return nil }, func(*sql.Tx) error { return errors.New("other") }} {
		if _, err := service.EnqueueBatch(t.Context(), requests, mutation); !errors.Is(err, ErrConflict) {
			t.Fatal("mixed batch created partial notifications", err)
		}
	}
	receipts, err := service.EnqueueBatch(t.Context(), requests, func(*sql.Tx) error { return ErrSkipMutation })
	if err != nil || len(receipts) != 2 || !receipts[0].Skipped || !receipts[1].Skipped {
		t.Fatal("authoritative skip was not preserved", err)
	}
	for _, invalid := range [][]Request{nil, {enqueueRequest("same"), enqueueRequest("same")}, {enqueueRequest("a"), enqueueRequest("b"), enqueueRequest("c")}} {
		if _, err := service.EnqueueBatch(t.Context(), invalid, nil); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("invalid batch accepted", err)
		}
	}
}
