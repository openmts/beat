package notificationqueue

import (
	"database/sql"
	"errors"
	"testing"
)

func lifecycleRequest() Request {
	return Request{SourceKind: "node_lifecycle", SourceKey: "node_lifecycle:fixture:period:node.expired:1", SourceID: "event", EventKind: "node.expired", Subject: "Expiry", Text: "Monitoring continues", UseSubscriptions: true}
}

func TestLifecycleMutationOptionalEventAndAtomicReceipt(t *testing.T) {
	service, database := queueFixture(t)
	receipt, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(tx *sql.Tx) (*Request, error) {
		_, err := tx.Exec(`UPDATE site_settings SET site_title='metadata' WHERE id=1`)
		return nil, err
	}})
	if err != nil || !receipt.Skipped {
		t.Fatal(receipt, err)
	}
	request := lifecycleRequest()
	receipt, err = service.MutateLifecycle(t.Context(), LifecycleMutation{
		Apply: func(tx *sql.Tx) (*Request, error) {
			_, err := tx.Exec(`UPDATE site_settings SET site_title='event' WHERE id=1`)
			return &request, err
		},
		Complete: func(tx *sql.Tx, receipt Receipt) error {
			if receipt.MessageID == "" || receipt.RoutingState != "not_routed" {
				t.Fatal(receipt)
			}
			_, err := tx.Exec(`UPDATE site_settings SET site_title=? WHERE id=1`, receipt.MessageID)
			return err
		},
	})
	if err != nil || receipt.Skipped {
		t.Fatal(receipt, err)
	}
	var title string
	if err = database.DB.QueryRow(`SELECT site_title FROM site_settings WHERE id=1`).Scan(&title); err != nil || title != receipt.MessageID {
		t.Fatal("receipt not atomic", title, err)
	}
}

func TestLifecycleMutationErrorsRollBackAndDuplicateRevalidates(t *testing.T) {
	service, database := queueFixture(t)
	if _, err := service.MutateLifecycle(t.Context(), LifecycleMutation{}); err != ErrInvalidRequest {
		t.Fatal(err)
	}
	request := lifecycleRequest()
	sentinel := errors.New("permission revoked")
	if _, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return nil, sentinel }}); err != sentinel {
		t.Fatal(err)
	}
	for _, kind := range []string{"source", "event", "scalar", "complete"} {
		t.Run(kind, func(t *testing.T) {
			invalid := request
			switch kind {
			case "source":
				invalid.SourceKind = "operation"
			case "event":
				invalid.EventKind = "operation.completed"
			case "scalar":
				invalid.SourceKey = ""
			}
			_, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return &invalid, nil }, Complete: func(*sql.Tx, Receipt) error { return sentinel }})
			if err == nil {
				t.Fatal("failure committed")
			}
			var count int
			if err = database.DB.QueryRow(`SELECT COUNT(*) FROM notification_messages`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
	if _, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return &request, nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return &request, sentinel }}); err != sentinel {
		t.Fatal("duplicate bypassed authority", err)
	}
	receipt, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return &request, nil }})
	if err != nil || !receipt.Duplicate {
		t.Fatal(receipt, err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return &request, nil }}); err == nil {
		t.Fatal("closed database accepted")
	}
}

func TestLifecycleMutationCommitAndMessageFailures(t *testing.T) {
	for _, stage := range []string{"lookup", "message", "commit"} {
		t.Run(stage, func(t *testing.T) {
			service, database := queueFixture(t)
			switch stage {
			case "lookup":
				if _, err := database.DB.Exec(`ALTER TABLE notification_messages RENAME COLUMN source_key TO missing_source_key`); err != nil {
					t.Fatal(err)
				}
			case "message":
				if _, err := database.DB.Exec(`CREATE TRIGGER fail BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'fail');END`); err != nil {
					t.Fatal(err)
				}
			case "commit":
				injectOutboxCommitFailure(t, queueFaultFixture{service: service, db: database.DB})
			}
			request := lifecycleRequest()
			if _, err := service.MutateLifecycle(t.Context(), LifecycleMutation{Apply: func(*sql.Tx) (*Request, error) { return &request, nil }}); err == nil {
				t.Fatal("failure accepted")
			}
		})
	}
}
