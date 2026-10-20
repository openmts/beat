package handler

import (
	"errors"
	"net/http"

	"github.com/beat/backend/internal/alerteval"
)

func respondAlertScopeCapacity(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, alerteval.ErrCapacity) {
		return false
	}
	evaluationError(w, err)
	return true
}
