package agentrollouthttp

import (
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/agentrollout"
)

func transferDeadline(w http.ResponseWriter, deadline time.Time, read bool) error {
	controller := http.NewResponseController(w)
	if read {
		if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return agentrollout.ErrUnavailable
		}
	}
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return agentrollout.ErrUnavailable
	}
	return nil
}
