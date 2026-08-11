package store

import (
	"database/sql"
	"errors"
	"sync"
)

var (
	ErrAdminUserNotFound    = errors.New("store: administrator not found")
	ErrAdminSessionNotFound = errors.New("store: administrator session not found")
	ErrLastOwner            = errors.New("store: at least one enabled owner is required")
)

type AdminStore struct {
	db          *sql.DB
	userMu      sync.Mutex
	bootstrapMu sync.Mutex
}

func NewAdminStore(db *sql.DB) *AdminStore {
	return &AdminStore{db: db}
}
