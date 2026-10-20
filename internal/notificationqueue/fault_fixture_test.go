package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/beat/backend/internal/secretbox"
)

type queueFaultFixture struct {
	service *Service
	db      *sql.DB
	actor   Actor
	receipt Receipt
}

func newQueueFaultFixture(t *testing.T) queueFaultFixture {
	t.Helper()
	s, db := queueFixture(t)
	fixture := queueFaultFixture{service: s, db: db.DB, actor: queueOwner(t, s)}
	addQueueChannel(t, s, "channel")
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("seed"), nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture.receipt = receipt
	return fixture
}

func (fixture queueFaultFixture) sql(t *testing.T, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := fixture.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func (fixture queueFaultFixture) lease(t *testing.T) deliveryLease {
	t.Helper()
	lease, err := fixture.service.claim(t.Context(), "worker")
	if err != nil || lease == nil {
		t.Fatalf("claim fixture delivery: %v", err)
	}
	return *lease
}

func (fixture queueFaultFixture) assertCount(t *testing.T, query string, want int) {
	t.Helper()
	var count int
	if err := fixture.db.QueryRow(query).Scan(&count); err != nil || count != want {
		t.Fatalf("expected row count %d, actual %d: %v", want, count, err)
	}
}

type unavailableEntropy struct{}

func (unavailableEntropy) Read([]byte) (int, error) { return 0, ErrQueueUnavailable }

type failingResourceService struct {
	base              secretbox.ResourceService
	seal, open, write bool
}

func (service failingResourceService) WithWrite(ctx context.Context, callback func(secretbox.ResourceAccess) error) error {
	if service.write {
		return ErrQueueUnavailable
	}
	return service.base.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		return callback(failingResourceAccess{base: access, seal: service.seal, open: service.open})
	})
}

func (service failingResourceService) OpenResource(ctx context.Context, value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if service.open {
		return nil, ErrQueueUnavailable
	}
	return service.base.OpenResource(ctx, value, aad)
}

type failingResourceAccess struct {
	base       secretbox.ResourceAccess
	seal, open bool
}

func (access failingResourceAccess) Seal(value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if access.seal {
		return nil, errors.New("injected secret sealing failure")
	}
	return access.base.Seal(value, aad)
}

func (access failingResourceAccess) Open(value []byte, aad secretbox.ResourceAAD) ([]byte, error) {
	if access.open {
		return nil, errors.New("injected secret opening failure")
	}
	return access.base.Open(value, aad)
}
