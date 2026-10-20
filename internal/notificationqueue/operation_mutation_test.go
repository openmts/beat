package notificationqueue

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type operationMutationFixture struct {
	service *Service
	db      *sql.DB
	actor   Actor
}

func operationFixture(t *testing.T) operationMutationFixture {
	t.Helper()
	service, database := queueFixture(t)
	fixture := operationMutationFixture{service: service, db: database.DB, actor: queueOwner(t, service)}
	if _, err := fixture.db.Exec("UPDATE site_settings SET site_title='Before operation' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func operationRequest() Request {
	return Request{SourceKey: "operation:mutation-fixture", SourceKind: "operation", SourceID: "mutation-fixture", EventKind: "operation.completed", Subject: "Operation complete", Text: "private operation result", UseSubscriptions: true}
}

func (fixture operationMutationFixture) write(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, "UPDATE site_settings SET site_title='Operation committed' WHERE id=1"); err != nil {
		return err
	}
	return fixture.service.audit(ctx, tx, auditInput{rule: authorityRule{actor: fixture.actor, owner: true, recent: true}, action: "operation.mutation.fixture", resourceType: "operation", resourceID: "fixture", detail: map[string]any{"state": "completed"}})
}

func (fixture operationMutationFixture) assert(t *testing.T, committed bool, counts [2]int) {
	t.Helper()
	var title string
	if err := fixture.db.QueryRow("SELECT site_title FROM site_settings WHERE id=1").Scan(&title); err != nil {
		t.Fatal(err)
	}
	expected := "Before operation"
	audits := 0
	if committed {
		expected = "Operation committed"
		audits = 1
	}
	if title != expected {
		t.Fatalf("expected site state %q, got %q", expected, title)
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM admin_audit_events WHERE action='operation.mutation.fixture'", audits},
		{"SELECT COUNT(*) FROM notification_messages", counts[0]},
		{"SELECT COUNT(*) FROM notification_deliveries", counts[1]},
	} {
		var actual int
		if err := fixture.db.QueryRow(check.query).Scan(&actual); err != nil || actual != check.want {
			t.Fatalf("expected count %d, got %d: %v", check.want, actual, err)
		}
	}
}

func (fixture operationMutationFixture) channels(t *testing.T) {
	t.Helper()
	addQueueChannel(t, fixture.service, "first")
	addQueueChannel(t, fixture.service, "second")
	if _, err := fixture.db.Exec(`UPDATE alert_channels SET event_kinds_json='["operation.completed","operation.failed"]'`); err != nil {
		t.Fatal(err)
	}
}

func TestMutateOperationOptionalEventCommitsOnlyStateAndAudit(t *testing.T) {
	fixture := operationFixture(t)
	err := fixture.service.MutateOperation(t.Context(), func(tx *sql.Tx, _ secretbox.ResourceAccess) (*Request, error) {
		return nil, fixture.write(t.Context(), tx)
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.assert(t, true, [2]int{0, 0})
}

func TestMutateOperationCommitsEncryptedOutboxWithAtomicFanout(t *testing.T) {
	fixture := operationFixture(t)
	fixture.channels(t)
	request := operationRequest()
	err := fixture.service.MutateOperation(t.Context(), func(tx *sql.Tx, _ secretbox.ResourceAccess) (*Request, error) {
		return &request, fixture.write(t.Context(), tx)
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.assert(t, true, [2]int{1, 2})
	receipt, err := fixture.service.SourceReceipt(t.Context(), request.SourceKey)
	if err != nil || receipt.State != "pending" || len(receipt.Deliveries) != 2 {
		t.Fatal("outbox fanout missing")
	}
	var encrypted []byte
	if err := fixture.db.QueryRow("SELECT payload_encrypted FROM notification_messages WHERE id=?", receipt.MessageID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if !secretbox.IsV2Envelope(encrypted) || bytes.Contains(encrypted, []byte(request.Text)) {
		t.Fatal("plaintext result persisted")
	}
	plain, err := fixture.service.secrets.OpenResource(t.Context(), encrypted, secretbox.NotificationMessageAAD(receipt.MessageID))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	var payload model.NotificationPayload
	if err := json.Unmarshal(plain, &payload); err != nil || payload.Text != request.Text {
		t.Fatal("encrypted result lost")
	}
}

func TestMutateOperationRollsBackAtEveryWriteBoundary(t *testing.T) {
	for _, failure := range []string{"business", "audit", "message", "delivery", "seal", "callback_seal", "commit", "receipt"} {
		t.Run(failure, func(t *testing.T) {
			fixture := operationFixture(t)
			fixture.channels(t)
			switch failure {
			case "audit", "message", "delivery":
				tables := map[string]string{"audit": "admin_audit_events", "message": "notification_messages", "delivery": "notification_deliveries"}
				if _, err := fixture.db.Exec("CREATE TRIGGER reject_mutation BEFORE INSERT ON " + tables[failure] + " BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
					t.Fatal(err)
				}
			case "seal", "callback_seal":
				fixture.service.secrets = failingResourceService{base: fixture.service.secrets, seal: true}
			case "commit":
				injectOutboxCommitFailure(t, queueFaultFixture{service: fixture.service, db: fixture.db, actor: fixture.actor})
			case "receipt":
				if _, err := fixture.db.Exec("ALTER TABLE notification_messages RENAME COLUMN source_key TO unavailable_source"); err != nil {
					t.Fatal(err)
				}
			}
			request := operationRequest()
			err := fixture.service.MutateOperation(t.Context(), func(tx *sql.Tx, access secretbox.ResourceAccess) (*Request, error) {
				if err := fixture.write(t.Context(), tx); err != nil {
					return nil, err
				}
				if failure == "business" {
					return nil, ErrConflict
				}
				if failure == "callback_seal" {
					_, err := access.Seal([]byte("private output"), secretbox.OperationOutputAAD("attempt", "stdout"))
					return nil, err
				}
				return &request, nil
			})
			if err == nil {
				t.Fatal("failed transaction reported success")
			}
			fixture.assert(t, false, [2]int{0, 0})
		})
	}
}

func TestMutateOperationInvalidEventsRollBackBusinessChanges(t *testing.T) {
	for _, change := range []func(*Request){
		func(r *Request) { r.SourceKind = "alert" },
		func(r *Request) { r.EventKind = "alert.triggered" },
		func(r *Request) { r.Subject = "invalid\nsubject" },
	} {
		fixture := operationFixture(t)
		request := operationRequest()
		change(&request)
		err := fixture.service.MutateOperation(t.Context(), func(tx *sql.Tx, _ secretbox.ResourceAccess) (*Request, error) {
			return &request, fixture.write(t.Context(), tx)
		})
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("expected invalid request, got %v", err)
		}
		fixture.assert(t, false, [2]int{0, 0})
	}
}

func TestOperationDuplicateSourceStillRechecksCurrentAuthority(t *testing.T) {
	for _, api := range []string{"mutate", "enqueue"} {
		t.Run(api, func(t *testing.T) {
			fixture := operationFixture(t)
			request := operationRequest()
			if err := fixture.service.MutateOperation(t.Context(), func(tx *sql.Tx, _ secretbox.ResourceAccess) (*Request, error) {
				return &request, fixture.write(t.Context(), tx)
			}); err != nil {
				t.Fatal(err)
			}
			receipt, err := fixture.service.SourceReceipt(t.Context(), request.SourceKey)
			if err != nil || receipt.State != "not_routed" {
				t.Fatal("unrouted event not recorded")
			}
			calls := 0
			verify := func(tx *sql.Tx, _ secretbox.ResourceAccess) error {
				calls++
				_, err := fixture.service.authorize(t.Context(), tx, authorityRule{actor: fixture.actor, owner: true, recent: true})
				return err
			}
			invoke := func() error {
				if api == "enqueue" {
					_, err := fixture.service.EnqueueOperation(t.Context(), request, verify)
					return err
				}
				return fixture.service.MutateOperation(t.Context(), func(tx *sql.Tx, access secretbox.ResourceAccess) (*Request, error) {
					return &request, verify(tx, access)
				})
			}
			if err := invoke(); err != nil || calls != 1 {
				t.Fatal("duplicate did not revalidate")
			}
			if _, err := fixture.db.Exec("UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP WHERE id=?", fixture.actor.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := invoke(); !errors.Is(err, ErrForbidden) || calls != 2 {
				t.Fatal("duplicate bypassed current authorization")
			}
			fixture.assert(t, true, [2]int{1, 0})
			after, err := fixture.service.SourceReceipt(t.Context(), request.SourceKey)
			if err != nil || after.MessageID != receipt.MessageID {
				t.Fatal("duplicate created another message")
			}
		})
	}
}

func TestMutateOperationRejectsMissingMutationAndUnavailableDependencies(t *testing.T) {
	fixture := operationFixture(t)
	if err := fixture.service.MutateOperation(t.Context(), nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("nil mutation accepted")
	}
	called := false
	callback := func(*sql.Tx, secretbox.ResourceAccess) (*Request, error) { called = true; return nil, nil }
	original := fixture.service.secrets
	fixture.service.secrets = failingResourceService{base: original, write: true}
	if err := fixture.service.MutateOperation(t.Context(), callback); err == nil || called {
		t.Fatal("secret lifecycle failure ignored")
	}
	fixture.service.secrets = original
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.MutateOperation(t.Context(), callback); !errors.Is(err, ErrQueueUnavailable) || called {
		t.Fatal("closed database admitted mutation")
	}
}

func TestMutateOperationOnlyFreshDurableOutboxRestoresReadiness(t *testing.T) {
	for _, mode := range []string{"no_event", "duplicate", "fresh"} {
		t.Run(mode, func(t *testing.T) {
			fixture := operationFixture(t)
			request := operationRequest()
			invoke := func(event *Request) error {
				return fixture.service.MutateOperation(t.Context(), func(*sql.Tx, secretbox.ResourceAccess) (*Request, error) { return event, nil })
			}
			if err := invoke(&request); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.db.Exec("CREATE TRIGGER reject_operation_message BEFORE INSERT ON notification_messages BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
				t.Fatal(err)
			}
			failed := request
			failed.SourceKey = "operation:failed-write"
			if err := invoke(&failed); !errors.Is(err, ErrQueueUnavailable) {
				t.Fatal("outbox failure was not surfaced")
			}
			if err := fixture.service.Ready(t.Context()); !errors.Is(err, ErrQueueUnavailable) {
				t.Fatal("failed outbox remained ready")
			}
			if _, err := fixture.db.Exec("DROP TRIGGER reject_operation_message"); err != nil {
				t.Fatal(err)
			}
			event := &request
			if mode == "no_event" {
				event = nil
			}
			if mode == "fresh" {
				fresh := request
				fresh.SourceKey = "operation:recovered"
				event = &fresh
			}
			if err := invoke(event); err != nil {
				t.Fatal(err)
			}
			err := fixture.service.Ready(t.Context())
			if mode == "fresh" && err != nil {
				t.Fatal("fresh durable outbox did not restore readiness")
			}
			if mode != "fresh" && !errors.Is(err, ErrQueueUnavailable) {
				t.Fatal("non-outbox mutation falsely restored readiness")
			}
		})
	}
}
