package serviceprincipalhttp

import (
	"net/http"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/store"
)

type createRequest struct {
	Name        string    `json:"name"`
	Purpose     string    `json:"purpose"`
	Scopes      []string  `json:"scopes"`
	Resources   []string  `json:"resources"`
	Visibility  string    `json:"visibility"`
	SourceCIDRs []string  `json:"source_cidrs"`
	ExpiresAt   time.Time `json:"expires_at"`
	Extended    bool      `json:"extended"`
}

type rotateRequest struct {
	createRequest
	OverlapSeconds int `json:"overlap_seconds"`
}

type revokeRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(store.ServicePrincipalActor) (any, error) {
		values, err := h.service.List(r.Context())
		if err != nil {
			return nil, err
		}
		return struct {
			Items []model.ServicePrincipal `json:"items"`
		}{values}, nil
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(actor store.ServicePrincipalActor) (any, error) {
		var input createRequest
		if err := decode(r, &input); err != nil {
			return nil, err
		}
		return h.service.Create(r.Context(), actor, input.toCreate())
	})
}

func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(actor store.ServicePrincipalActor) (any, error) {
		var input rotateRequest
		if err := decode(r, &input); err != nil {
			return nil, err
		}
		if input.OverlapSeconds < 0 {
			return nil, ErrInvalid
		}
		return h.service.Rotate(r.Context(), actor, r.PathValue("token_id"), input.toRotate())
	})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	h.admin(w, r, func(actor store.ServicePrincipalActor) (any, error) {
		var input revokeRequest
		if err := decode(r, &input); err != nil {
			return nil, err
		}
		err := h.service.Revoke(r.Context(), actor, serviceprincipal.RevokeInput{
			ID: r.PathValue("token_id"), Reason: input.Reason})
		if err != nil {
			return nil, err
		}
		return struct {
			Revoked bool `json:"revoked"`
		}{true}, nil
	})
}

func (request createRequest) toCreate() serviceprincipal.CreateInput {
	return serviceprincipal.CreateInput{Name: request.Name, Purpose: request.Purpose, Scopes: scopes(request.Scopes),
		Resources: resources(request.Resources), Visibility: request.Visibility, SourceCIDRs: request.SourceCIDRs,
		ExpiresAt: request.ExpiresAt, Extended: request.Extended}
}

func (request rotateRequest) toRotate() serviceprincipal.RotateInput {
	return serviceprincipal.RotateInput{Name: request.Name, Purpose: request.Purpose, Scopes: scopes(request.Scopes),
		Resources: resources(request.Resources), Visibility: request.Visibility, SourceCIDRs: request.SourceCIDRs,
		ExpiresAt: request.ExpiresAt, Extended: request.Extended,
		Overlap: time.Duration(request.OverlapSeconds) * time.Second}
}

// resources 接受 "kind:id" 形式，避免为每个约束引入嵌套对象。
func resources(values []string) []model.ServiceResource {
	if values == nil {
		return nil
	}
	out := make([]model.ServiceResource, 0, len(values))
	for _, value := range values {
		kind, id, ok := splitResource(value)
		if !ok {
			out = append(out, model.ServiceResource{ID: value})
			continue
		}
		out = append(out, model.ServiceResource{Kind: model.ServiceResourceKind(kind), ID: id})
	}
	return out
}

func splitResource(value string) (string, string, bool) {
	for index := 0; index < len(value); index++ {
		if value[index] == ':' {
			return value[:index], value[index+1:], true
		}
	}
	return "", "", false
}

func scopes(values []string) []model.ServiceScope {
	if values == nil {
		return nil
	}
	out := make([]model.ServiceScope, 0, len(values))
	for _, value := range values {
		out = append(out, model.ServiceScope(value))
	}
	return out
}
