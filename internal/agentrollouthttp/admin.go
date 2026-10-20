package agentrollouthttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/agentrolloutremote"
)

func (h *Handler) listReleases(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		q, err := page(r)
		if err != nil {
			return nil, err
		}
		return h.service.ListReleases(r.Context(), a, q)
	})
}

func (h *Handler) remoteReleases(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(agentrollout.Actor) (any, error) {
		if h.remote == nil {
			return nil, agentrollout.ErrDisabled
		}
		page, err := remotePage(r)
		if err != nil {
			return nil, err
		}
		return h.remote.List(r.Context(), page)
	})
}

func (h *Handler) syncRemoteRelease(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		if h.remote == nil {
			return nil, agentrollout.ErrDisabled
		}
		return h.remote.Sync(r.Context(), a, r.PathValue("tag"))
	})
}

func remotePage(r *http.Request) (int, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) > 1 || len(values["page"]) > 1 || len(values) == 1 && len(values["page"]) == 0 {
		return 0, agentrolloutremote.ErrInvalid
	}
	value := values.Get("page")
	if value == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 || page > 100 {
		return 0, agentrolloutremote.ErrInvalid
	}
	return page, nil
}

func (h *Handler) listRollouts(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		q, err := page(r)
		if err != nil {
			return nil, err
		}
		return h.service.ListRollouts(r.Context(), a, q)
	})
}

func (h *Handler) targets(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		q, err := page(r)
		if err != nil {
			return nil, err
		}
		return h.service.ListTargets(r.Context(), a, q)
	})
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		return h.service.ReleaseDetail(r.Context(), a, r.PathValue("release_id"))
	})
}

func (h *Handler) rollout(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		return h.service.GetRollout(r.Context(), a, r.PathValue("rollout_id"))
	})
}

func (h *Handler) importManifest(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		var input protocol.SignedManifest
		if err := decode(r, &input); err != nil {
			return nil, err
		}
		return h.service.Import(r.Context(), a, input)
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		var input agentrollout.CreateInput
		if err := decode(r, &input); err != nil {
			return nil, err
		}
		return h.service.Create(r.Context(), a, input)
	})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		return nil, h.service.RevokeRelease(r.Context(), a, r.PathValue("release_id"))
	})
}

func (h *Handler) deleteRelease(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		return nil, h.service.DeleteRelease(r.Context(), a, r.PathValue("release_id"))
	})
}

func (h *Handler) deleteRollout(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		return nil, h.service.DeleteRollout(r.Context(), a, r.PathValue("rollout_id"))
	})
}

func (h *Handler) control(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.admin(w, r, func(a agentrollout.Actor) (any, error) {
			id := r.PathValue("rollout_id")
			switch action {
			case "promote":
				return nil, h.service.Promote(r.Context(), a, id)
			case "pause":
				return nil, h.service.Pause(r.Context(), a, id)
			case "abort":
				return nil, h.service.Abort(r.Context(), a, id)
			default:
				var input struct {
					Acknowledge bool `json:"acknowledge_uncertainty"`
				}
				if err := decode(r, &input); err != nil {
					return nil, err
				}
				return nil, h.service.Resume(r.Context(), a, agentrollout.ResumeInput{ID: id, AcknowledgeUncertainty: input.Acknowledge})
			}
		})
	}
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) {
		if r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			return nil, agentrollout.ErrInvalid
		}
		select {
		case h.uploads <- struct{}{}:
		default:
			return nil, agentrollout.ErrBusy
		}
		defer func() { <-h.uploads }()
		if err := transferDeadline(w, timeNow().Add(transferTimeout), true); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(r.Context(), transferTimeout)
		defer cancel()
		return nil, h.service.PutArtifact(ctx, a, agentrollout.ArtifactUpload{ReleaseID: r.PathValue("release_id"), ArtifactID: r.PathValue("artifact_id"), Body: r.Body})
	})
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(a agentrollout.Actor) (any, error) { return h.service.Status(r.Context(), a) })
}
