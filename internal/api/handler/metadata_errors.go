package handler

import (
	"errors"
	"net/http"

	"github.com/beat/backend/internal/store"
)

func respondMetadataMutationError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrMetadataForbidden) {
		JSONError(w, http.StatusForbidden, "administrator permission is required")
		return true
	}
	if errors.Is(err, store.ErrMetadataRecentAuth) {
		JSONError(w, http.StatusPreconditionRequired, "recent authentication is required")
		return true
	}
	return false
}
