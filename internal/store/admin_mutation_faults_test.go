package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestAdministratorMutationCommitAndPostWriteFaults(t *testing.T) {
	for _, fault := range []string{"commit", "changed result", "session write", "password write", "delete write"} {
		t.Run(fault, func(t *testing.T) {
			f := newAdministratorFixture(t)
			user := testAdminUser("target", model.AdminRoleAdmin)
			change := f.change(AdministratorCreate, user)
			switch fault {
			case "commit":
				administratorTrigger(t, f, "CREATE TABLE commit_guard(parent TEXT REFERENCES admin_users(id) DEFERRABLE INITIALLY DEFERRED)")
				administratorTrigger(t, f, "CREATE TRIGGER fail_commit AFTER INSERT ON admin_audit_events BEGIN INSERT INTO commit_guard VALUES('missing'); END")
			case "changed result":
				administratorTrigger(t, f, "CREATE TRIGGER corrupt_create AFTER INSERT ON admin_users BEGIN UPDATE admin_users SET display_name='unexpected' WHERE id=NEW.id; END")
			case "session write":
				second := administratorSession(f.principal.User.ID, "other-session", f.now)
				if err := f.store.CreateSession(t.Context(), &second); err != nil {
					t.Fatal(err)
				}
				administratorTrigger(t, f, "CREATE TRIGGER fail_session BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(ABORT,'session fault'); END")
				change = f.change(AdministratorPassword, f.principal.User)
				change.ExpectedPasswordHash = change.User.PasswordHash
				change.User.PasswordHash = "new-hash"
			case "password write":
				administratorTrigger(t, f, "CREATE TRIGGER fail_password BEFORE UPDATE ON admin_users BEGIN SELECT RAISE(IGNORE); END")
				change = f.change(AdministratorPassword, f.principal.User)
				change.ExpectedPasswordHash = change.User.PasswordHash
				change.User.PasswordHash = "new-hash"
			case "delete write":
				if err := f.store.CreateUser(t.Context(), &user); err != nil {
					t.Fatal(err)
				}
				administratorTrigger(t, f, "CREATE TRIGGER fail_delete BEFORE DELETE ON admin_users BEGIN SELECT RAISE(IGNORE); END")
				change = f.change(AdministratorDelete, user)
			}
			if _, err := f.store.ChangeAdministrator(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
				t.Fatalf("fault %s: %v", fault, err)
			}
			if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 0 {
				t.Fatal("failed operation committed success audit")
			}
		})
	}
}

func TestAdministratorMutationTransactionalHelperErrors(t *testing.T) {
	f := newAdministratorFixture(t)
	change := f.change(AdministratorUpdate, f.principal.User)
	tx, err := f.store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = applyAdministratorChange(t.Context(), tx, change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if _, err = readAdministratorResult(t.Context(), tx, change.User); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = ensureTransactionalOwner(t.Context(), tx, change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = prepareAdministratorAudit(t.Context(), tx, &change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = authorizeAdministratorChange(t.Context(), tx, &change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = oneAdministratorWrite(nil, sql.ErrTxDone); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = oneAdministratorWrite(mutationBadResult{}, nil); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = f.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.ChangeAdministrator(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
}

type mutationBadResult struct{}

func (mutationBadResult) LastInsertId() (int64, error) { return 0, sql.ErrTxDone }

func (mutationBadResult) RowsAffected() (int64, error) { return 0, sql.ErrTxDone }

func TestAdministratorMutationHelperMissingTargets(t *testing.T) {
	f := newAdministratorFixture(t)
	tx, err := f.store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	change := f.change(AdministratorUpdate, testAdminUser("missing", model.AdminRoleAdmin))
	if _, err := applyAdministratorChange(t.Context(), tx, change); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatal(err)
	}
	change = f.change("unknown", f.principal.User)
	if _, err := applyAdministratorChange(t.Context(), tx, change); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal(err)
	}
	change = f.change(AdministratorUpdate, f.principal.User)
	change.User.Role = "invalid"
	if _, err := applyAdministratorChange(t.Context(), tx, change); err == nil {
		t.Fatal("invalid role accepted")
	}
}
