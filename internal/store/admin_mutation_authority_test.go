package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAdministratorMutationRechecksAuthority(t *testing.T) {
	for _, test := range []struct {
		name, sql string
		want      error
	}{
		{"revoked session", "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP", ErrAdminSessionNotFound},
		{"expired proof", "UPDATE admin_sessions SET reauthenticated_until=NULL", ErrAdminMutationRecent},
		{"demoted actor", "UPDATE admin_users SET role='admin'", ErrAdminMutationDenied},
		{"disabled actor", "UPDATE admin_users SET enabled=0", ErrAdminMutationDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAdministratorFixture(t)
			administratorTrigger(t, f, test.sql)
			_, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorCreate, testAdminUser("new", model.AdminRoleAdmin)))
			if !errors.Is(err, test.want) {
				t.Fatalf("want %v, got %v", test.want, err)
			}
			if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 0 {
				t.Fatal("denial committed audit as success")
			}
		})
	}
}

func TestAdministratorMutationGuardsAndWriteFaults(t *testing.T) {
	f := newAdministratorFixture(t)
	if _, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorDelete, f.principal.User)); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal(err)
	}
	last := f.principal.User
	last.Role = model.AdminRoleAdmin
	if _, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorUpdate, last)); !errors.Is(err, ErrLastOwner) {
		t.Fatal(err)
	}
	missing := testAdminUser("missing", model.AdminRoleAdmin)
	if _, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorUpdate, missing)); !errors.Is(err, ErrAdminUserNotFound) {
		t.Fatal(err)
	}
	invalid := f.change(AdministratorCreate, missing)
	invalid.Audit.RequestID = ""
	if _, err := f.store.ChangeAdministrator(t.Context(), invalid); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal(err)
	}
	invalid = f.change("invalid", missing)
	if _, err := f.store.ChangeAdministrator(t.Context(), invalid); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal(err)
	}
	invalid = f.change(AdministratorPassword, f.principal.User)
	if _, err := f.store.ChangeAdministrator(t.Context(), invalid); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal(err)
	}
	administratorTrigger(t, f, "CREATE TRIGGER ignore_create BEFORE INSERT ON admin_users BEGIN SELECT RAISE(IGNORE); END")
	if _, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorCreate, missing)); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	administratorTrigger(t, f, "DROP TRIGGER ignore_create")
	invalid = f.change(AdministratorCreate, missing)
	invalid.User.Role = "invalid"
	if _, err := f.store.ChangeAdministrator(t.Context(), invalid); err == nil {
		t.Fatal("invalid role accepted")
	}
}

func TestAdministratorMutationConcurrentLastOwner(t *testing.T) {
	f := newAdministratorFixture(t)
	second := testAdminUser("second", model.AdminRoleOwner)
	if err := f.store.CreateUser(t.Context(), &second); err != nil {
		t.Fatal(err)
	}
	session := administratorSession(second.ID, "second-session", f.now)
	if err := f.store.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	changes := []AdministratorChange{f.change(AdministratorUpdate, f.principal.User), f.change(AdministratorUpdate, second)}
	changes[1].Principal = model.AdminPrincipal{User: second, Session: session}
	changes[1].Audit.ActorID = second.ID
	start := make(chan struct{})
	results := make(chan error, len(changes))
	var tasks sync.WaitGroup
	for _, change := range changes {
		change.User.Role = model.AdminRoleAdmin
		tasks.Go(func() { <-start; _, err := f.store.ChangeAdministrator(t.Context(), change); results <- err })
	}
	close(start)
	tasks.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 || administratorCount(t, f, "SELECT count(*) FROM admin_users WHERE role='owner' AND enabled=1") != 1 {
		t.Fatalf("last owner invariant: successes=%d", successes)
	}
}

func TestAdministratorMutationDisplayNamePreservesSessions(t *testing.T) {
	f := newAdministratorFixture(t)
	next := f.principal.User
	next.DisplayName = "Renamed"
	updated, err := f.store.ChangeAdministrator(t.Context(), f.change(AdministratorUpdate, next))
	if err != nil || updated.DisplayName != "Renamed" {
		t.Fatal(err)
	}
	session, err := f.store.GetSessionByID(t.Context(), "current")
	if err != nil || !session.Active(f.now.Add(time.Second)) {
		t.Fatal("non-security edit revoked session")
	}
}
