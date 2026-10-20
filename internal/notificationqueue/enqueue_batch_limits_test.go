package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/secretbox"
)

func TestBatchAdmissionCountsAllFanoutBeforeBusinessMutation(t *testing.T) {
	service, db := queueFixture(t)
	addQueueChannel(t, service, "channel")
	// 真实 SQL 计数的虚拟满队列，避免写入十万条无关测试投递。
	if _, err := db.DB.Exec(`ALTER TABLE notification_deliveries RENAME TO saved_deliveries;
		CREATE VIEW notification_deliveries AS WITH RECURSIVE slots(n) AS
		(VALUES(1) UNION ALL SELECT n+1 FROM slots WHERE n<99999) SELECT 'pending' AS state FROM slots`); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := service.EnqueueBatch(t.Context(), []Request{enqueueRequest("one"), enqueueRequest("two")}, func(*sql.Tx) error { called = true; return nil })
	if !errors.Is(err, ErrQueueCapacity) || called {
		t.Fatal("batch fanout exceeded shared capacity or reached mutation", err)
	}
	if service.persistenceBroken.Load() {
		t.Fatal("capacity exhaustion marked persistence broken")
	}
}

type countedSecretWindow struct {
	secretbox.ResourceService
	calls atomic.Int64
}

func (window *countedSecretWindow) WithWrite(ctx context.Context, fn func(secretbox.ResourceAccess) error) error {
	window.calls.Add(1)
	return window.ResourceService.WithWrite(ctx, fn)
}

func TestBatchUsesOneSecretWindowAndExcludesRotation(t *testing.T) {
	service, db := queueFixture(t)
	coordinator := service.secrets.(*applicationsecrets.Service)
	window := &countedSecretWindow{ResourceService: coordinator}
	service.secrets = window
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := service.EnqueueBatch(t.Context(), []Request{enqueueRequest("one"), enqueueRequest("two")}, func(*sql.Tx) error { close(entered); <-release; return nil })
		finished <- err
	}()
	<-entered
	rotated := make(chan error, 1)
	go func() { _, err := coordinator.StartRotation(t.Context()); rotated <- err }()
	select {
	case err := <-rotated:
		close(release)
		<-finished
		t.Fatal("rotation passed batch secret window", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-rotated; err != nil {
		t.Fatal(err)
	}
	if window.calls.Load() != 1 {
		t.Fatal("batch opened nested or repeated secret windows")
	}
	rows, err := db.DB.Query("SELECT id,payload_encrypted FROM notification_messages")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var encrypted []byte
		if err := rows.Scan(&id, &encrypted); err != nil {
			t.Fatal(err)
		}
		plain, err := coordinator.OpenResource(t.Context(), encrypted, secretbox.NotificationMessageAAD(id))
		if err != nil {
			t.Fatal("rotation invalidated batch payload", err)
		}
		clear(plain)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestBatchSkipRollsBackAndDoesNotClearUnprovenPersistenceFailure(t *testing.T) {
	service, db := queueFixture(t)
	service.persistenceBroken.Store(true)
	if _, err := db.DB.Exec("CREATE TABLE batch_source(id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	receipts, err := service.EnqueueBatch(t.Context(), []Request{enqueueRequest("one"), enqueueRequest("two")}, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO batch_source VALUES('write')")
		if err != nil {
			return err
		}
		return ErrSkipMutation
	})
	if err != nil || len(receipts) != 2 || !receipts[0].Skipped || !receipts[1].Skipped || !service.persistenceBroken.Load() {
		t.Fatal("skip cleared persistence evidence or lost receipts", err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM batch_source").Scan(&count); err != nil || count != 0 {
		t.Fatal("skip committed business state", err)
	}
}
