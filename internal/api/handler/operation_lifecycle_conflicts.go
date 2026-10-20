package handler

import (
	"errors"
	"net/http"

	"github.com/beat/backend/internal/store"
)

func respondOperationLifecycleConflict(w http.ResponseWriter, err error) bool {
	if errors.Is(err, store.ErrOperationNodeBusy) || errors.Is(err, store.ErrOperationGroupBusy) {
		JSONError(w, http.StatusConflict, err.Error())
		return true
	}
	return false
}
