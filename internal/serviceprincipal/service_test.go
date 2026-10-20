package serviceprincipal

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func serviceActor() store.ServicePrincipalActor {
	return store.ServicePrincipalActor{UserID: "owner-one", SessionID: "session-abcdef", RequestID: "request-one"}
}

func createInput() CreateInput {
	return CreateInput{Name: "nightly backup", Purpose: "automation", Scopes: []model.ServiceScope{model.ServiceScopeBackupsCreate},
		Visibility: "private", SourceCIDRs: []string{"192.0.2.0/24"}, ExpiresAt: serviceNow().Add(24 * time.Hour)}
}

func serviceNow() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC) }

func newTestService(storage *fakeStore) *Service {
	service := NewService(storage)
	service.now = func() time.Time { return serviceNow() }
	service.ids = func() string { return "principal-one" }
	return service
}

func TestServiceCreateIssuesDisplayableCredential(t *testing.T) {
	storage := newFakeStore()
	service := newTestService(storage)
	credential, err := service.Create(t.Context(), serviceActor(), createInput())
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(credential.Principal.TokenHash, credential.Token) {
		t.Fatal("issued credential does not match its stored digest")
	}
	if prefix, ok := DisplayPrefix(credential.Token); !ok || prefix != credential.Principal.TokenPrefix {
		t.Fatal("issued credential prefix is invalid")
	}
	if len(storage.created) != 1 || storage.actors[0].RequestID != "request-one" {
		t.Fatal("creation was not persisted with its actor")
	}
	if storage.created[0].CreatedBy != "owner-one" || storage.created[0].Revision != 1 {
		t.Fatal("creation metadata is incomplete", storage.created[0])
	}
}

func TestServiceCreateRejectsInvalidPolicyAndAnonymousActors(t *testing.T) {
	storage := newFakeStore()
	service := newTestService(storage)
	input := createInput()
	input.Name = "  "
	if _, err := service.Create(t.Context(), serviceActor(), input); !errors.Is(err, ErrName) {
		t.Fatal("blank name was accepted", err)
	}
	input = createInput()
	input.ExpiresAt = serviceNow().Add(-time.Hour)
	if _, err := service.Create(t.Context(), serviceActor(), input); !errors.Is(err, ErrExpiry) {
		t.Fatal("past expiry was accepted", err)
	}
	if _, err := service.Create(t.Context(), store.ServicePrincipalActor{}, createInput()); !errors.Is(err, ErrStorage) {
		t.Fatal("anonymous actor was accepted", err)
	}
	if len(storage.created) != 0 {
		t.Fatal("rejected creation reached storage")
	}
}

func TestServiceCreateClassifiesStorageFailures(t *testing.T) {
	cases := map[string]struct{ failure, expected error }{
		"not found": {store.ErrServicePrincipalNotFound, ErrPrincipalNotFound},
		"revoked":   {store.ErrServicePrincipalRevoked, ErrPrincipalRevoked},
		"rotation":  {store.ErrServicePrincipalRotation, ErrRotationConflict},
		"other":     {errors.New("disk unavailable"), ErrStorage},
	}
	for name, test := range cases {
		storage := newFakeStore()
		storage.createErr = test.failure
		service := newTestService(storage)
		if _, err := service.Create(t.Context(), serviceActor(), createInput()); !errors.Is(err, test.expected) {
			t.Fatalf("%s mapped to %v", name, err)
		}
	}
}

func TestServiceRotateCarriesOverlapAndPredecessor(t *testing.T) {
	storage := newFakeStore()
	service := newTestService(storage)
	credential, err := service.Rotate(t.Context(), serviceActor(), "principal-old", RotateInput{
		Name: "rotated", Scopes: []model.ServiceScope{model.ServiceScopeHealthRead}, ExpiresAt: serviceNow().Add(time.Hour), Overlap: 30 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Principal.RotatedFrom != "principal-old" || credential.Principal.RotationOverlapSeconds != 1800 {
		t.Fatal("rotation metadata is incomplete", credential.Principal)
	}
	if len(storage.rotations) != 1 || storage.rotations[0].Previous != "principal-old" || storage.rotations[0].Overlap != 30*time.Minute {
		t.Fatal("rotation was not persisted with its window", storage.rotations)
	}
	if !Matches(credential.Principal.TokenHash, credential.Token) {
		t.Fatal("rotated credential does not match its digest")
	}
}

func TestServiceListStripsDigests(t *testing.T) {
	storage := newFakeStore(model.ServicePrincipal{ID: "principal-one", Name: "one", TokenHash: []byte("hash")})
	service := newTestService(storage)
	values, err := service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].TokenHash != nil {
		t.Fatal("management view exposed a digest", values)
	}
}

func TestServiceRevokeValidatesInput(t *testing.T) {
	storage := newFakeStore()
	service := newTestService(storage)
	if err := service.Revoke(t.Context(), serviceActor(), RevokeInput{}); !errors.Is(err, ErrPrincipalNotFound) {
		t.Fatal("empty identity was accepted", err)
	}
	if err := service.Revoke(t.Context(), serviceActor(), RevokeInput{ID: "principal-one", Reason: string(make([]byte, model.MaximumServiceTokenPurpose+1))}); !errors.Is(err, ErrPurpose) {
		t.Fatal("oversized reason was accepted", err)
	}
	if err := service.Revoke(t.Context(), serviceActor(), RevokeInput{ID: "principal-one", Reason: "done"}); err != nil {
		t.Fatal(err)
	}
	if len(storage.revocations) != 1 || storage.revocations[0].Actor.RequestID != "request-one" {
		t.Fatal("revocation lost its actor", storage.revocations)
	}
}

func TestServiceBulkOperationsMapFailures(t *testing.T) {
	storage := newFakeStore()
	storage.revokeAll, storage.expire = 2, 3
	service := newTestService(storage)
	if revoked, err := service.RevokeAll(t.Context(), serviceActor(), "restore"); revoked != 2 || err != nil {
		t.Fatal("bulk revocation returned unexpected result", revoked, err)
	}
	if revoked, err := service.Expire(t.Context(), serviceNow()); revoked != 3 || err != nil {
		t.Fatal("expiry returned unexpected result", revoked, err)
	}
	storage.revokeAllErr = errors.New("storage down")
	if _, err := service.RevokeAll(t.Context(), serviceActor(), "restore"); !errors.Is(err, ErrStorage) {
		t.Fatal("bulk revocation failure was not classified", err)
	}
}
