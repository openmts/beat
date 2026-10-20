package serviceprincipal

import (
	"context"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type fakeStore struct {
	values       map[string]model.ServicePrincipal
	createErr    error
	listErr      error
	revokeErr    error
	rotateErr    error
	revokeAllErr error
	expireErr    error
	revokeAll    int
	expire       int
	created      []model.ServicePrincipal
	rotations    []store.ServicePrincipalRotation
	revocations  []store.ServicePrincipalRevocation
	actors       []store.ServicePrincipalActor
	touched      []string
	touchErr     error
}

func newFakeStore(values ...model.ServicePrincipal) *fakeStore {
	storage := &fakeStore{values: map[string]model.ServicePrincipal{}}
	for _, value := range values {
		storage.values[value.ID] = value
	}
	return storage
}

func (storage *fakeStore) Create(_ context.Context, value model.ServicePrincipal, actor store.ServicePrincipalActor) error {
	if storage.createErr != nil {
		return storage.createErr
	}
	storage.created = append(storage.created, value)
	storage.actors = append(storage.actors, actor)
	storage.values[value.ID] = value
	return nil
}

func (storage *fakeStore) List(context.Context) ([]model.ServicePrincipal, error) {
	if storage.listErr != nil {
		return nil, storage.listErr
	}
	values := []model.ServicePrincipal{}
	for _, value := range storage.values {
		values = append(values, value)
	}
	return values, nil
}

func (storage *fakeStore) Revoke(_ context.Context, change store.ServicePrincipalRevocation) error {
	if storage.revokeErr != nil {
		return storage.revokeErr
	}
	storage.revocations = append(storage.revocations, change)
	return nil
}

func (storage *fakeStore) Rotate(_ context.Context, change store.ServicePrincipalRotation) error {
	if storage.rotateErr != nil {
		return storage.rotateErr
	}
	storage.rotations = append(storage.rotations, change)
	return nil
}

func (storage *fakeStore) RevokeAll(context.Context, store.ServicePrincipalBulkRevocation) (int, error) {
	return storage.revokeAll, storage.revokeAllErr
}

func (storage *fakeStore) Expire(context.Context, time.Time) (int, error) {
	return storage.expire, storage.expireErr
}

func (storage *fakeStore) ByHash(_ context.Context, hash []byte) (*model.ServicePrincipal, error) {
	if storage.listErr != nil {
		return nil, storage.listErr
	}
	for _, value := range storage.values {
		if Matches(value.TokenHash, model.ServiceTokenPrefix+"candidate") || string(value.TokenHash) == string(hash) {
			copy := value
			return &copy, nil
		}
	}
	return nil, store.ErrServicePrincipalNotFound
}

func (storage *fakeStore) RecordUse(_ context.Context, id string, _ time.Time) error {
	storage.touched = append(storage.touched, id)
	return storage.touchErr
}
