package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/oidc"
)

const oidcBrowserBindingCookie = "beat_oidc_binding"

// OIDCHandler 暴露 OIDC 登录、链接与提供方管理 API。
type OIDCHandler struct {
	service *oidc.Service
}

func NewOIDCHandler(service *oidc.Service) *OIDCHandler {
	return &OIDCHandler{service: service}
}

func (handler *OIDCHandler) HandleListPublicProviders(w http.ResponseWriter, request *http.Request) {
	providers, err := handler.service.ListPublicProviders(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "oidc providers are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, providers)
}

func (handler *OIDCHandler) HandleBeginLogin(w http.ResponseWriter, request *http.Request) {
	providerID := request.PathValue("id")
	var body struct {
		ReturnPath string `json:"return_path"`
	}
	if err := decodeOIDCBeginBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid oidc request")
		return
	}
	schemeHost := effectiveSchemeHost(request)
	result, err := handler.service.BeginLogin(request.Context(), providerID, body.ReturnPath,
		schemeHost, middleware.ClientIP(request), request.UserAgent())
	if err != nil {
		writeOIDCError(w, err)
		return
	}
	setOIDCBindingCookie(w, request, result.BrowserBinding.Value, result.BrowserBinding.MaxAge)
	JSONResponse(w, http.StatusOK, map[string]string{"authorization_url": result.AuthorizationURL})
}

func (handler *OIDCHandler) HandleCallback(w http.ResponseWriter, request *http.Request) {
	state := request.URL.Query().Get("state")
	code := request.URL.Query().Get("code")
	bindingCookie, err := request.Cookie(oidcBrowserBindingCookie)
	if err != nil {
		handler.redirectCallbackError(w, request, "login_failed")
		return
	}
	returnPath, token, err := handler.service.HandleCallback(request.Context(),
		state, code, bindingCookie.Value, middleware.ClientIP(request), request.UserAgent())
	if err != nil {
		slog.WarnContext(request.Context(), "oidc callback failed",
			"request_id", middleware.RequestID(request.Context()),
			"error_category", oidcErrorCategory(err))
		handler.redirectCallbackError(w, request, "login_failed")
		return
	}
	expireOIDCBindingCookie(w, request)
	if token != "" {
		setAdminSessionCookie(w, request, token, time.Now().UTC().Add(7*24*time.Hour))
	}
	http.Redirect(w, request, returnPath, http.StatusFound)
}

func (handler *OIDCHandler) HandleListIdentitiesMe(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	identities, err := handler.service.ListIdentitiesForUser(request.Context(), principal.User.ID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "external identities are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, identities)
}

func (handler *OIDCHandler) HandleBeginLink(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	providerID := request.PathValue("id")
	var body struct {
		ReturnPath string `json:"return_path"`
	}
	if err := decodeOIDCBeginBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid oidc request")
		return
	}
	schemeHost := effectiveSchemeHost(request)
	result, err := handler.service.BeginLink(request.Context(), &principal, providerID, body.ReturnPath,
		schemeHost, middleware.ClientIP(request), request.UserAgent())
	if err != nil {
		writeOIDCError(w, err)
		return
	}
	setOIDCBindingCookie(w, request, result.BrowserBinding.Value, result.BrowserBinding.MaxAge)
	JSONResponse(w, http.StatusOK, map[string]string{"authorization_url": result.AuthorizationURL})
}

func (handler *OIDCHandler) HandleUnlink(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	identityID := request.PathValue("id")
	if err := handler.service.Unlink(request.Context(), &principal, identityID,
		middleware.ClientIP(request), request.UserAgent()); err != nil {
		writeOIDCError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- 提供方管理（owner-only，最近重认证） ---

func (handler *OIDCHandler) HandleListProviders(w http.ResponseWriter, request *http.Request) {
	providers, err := handler.service.ListProviders(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "oidc providers are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, providers)
}

func (handler *OIDCHandler) HandleCreateProvider(w http.ResponseWriter, request *http.Request) {
	input, err := decodeProviderInput(w, request)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid provider configuration")
		return
	}
	provider, err := handler.service.CreateProvider(request.Context(), input,
		middleware.ClientIP(request), request.UserAgent())
	if err != nil {
		writeOIDCError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, provider)
}

func (handler *OIDCHandler) HandleUpdateProvider(w http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	input, err := decodeProviderInput(w, request)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid provider configuration")
		return
	}
	provider, err := handler.service.UpdateProvider(request.Context(), id, input,
		middleware.ClientIP(request), request.UserAgent())
	if err != nil {
		writeOIDCError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, provider)
}

func (handler *OIDCHandler) HandleDeleteProvider(w http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if err := handler.service.DeleteProvider(request.Context(), id,
		middleware.ClientIP(request), request.UserAgent()); err != nil {
		writeOIDCError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeProviderInput(w http.ResponseWriter, request *http.Request) (oidc.ProviderInput, error) {
	var body struct {
		Slug         string   `json:"slug"`
		DisplayName  string   `json:"display_name"`
		IssuerURL    string   `json:"issuer_url"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Scopes       []string `json:"scopes"`
		Enabled      bool     `json:"enabled"`
		SortOrder    int      `json:"sort_order"`
	}
	if err := decodeOIDCBeginBody(w, request, &body); err != nil {
		return oidc.ProviderInput{}, err
	}
	return oidc.ProviderInput{
		Slug: body.Slug, DisplayName: body.DisplayName, IssuerURL: body.IssuerURL,
		ClientID: body.ClientID, ClientSecret: body.ClientSecret, Scopes: body.Scopes,
		Enabled: body.Enabled, SortOrder: body.SortOrder,
	}, nil
}

func decodeOIDCBeginBody(w http.ResponseWriter, request *http.Request, destination any) error {
	return decodeAuthBody(w, request, destination)
}

func effectiveSchemeHost(request *http.Request) string {
	info := middleware.RequestInfoFrom(request)
	return info.Scheme + "://" + info.Host
}

func setOIDCBindingCookie(w http.ResponseWriter, request *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcBrowserBindingCookie, Value: value, Path: "/api/v1/auth/oidc/callback",
		MaxAge: maxAge, Expires: time.Now().UTC().Add(time.Duration(maxAge) * time.Second),
		HttpOnly: true, Secure: middleware.IsSecure(request), SameSite: http.SameSiteLaxMode,
	})
}

func expireOIDCBindingCookie(w http.ResponseWriter, request *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: oidcBrowserBindingCookie, Path: "/api/v1/auth/oidc/callback",
		MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true,
		Secure: middleware.IsSecure(request), SameSite: http.SameSiteLaxMode,
	})
}

func (handler *OIDCHandler) redirectCallbackError(w http.ResponseWriter, request *http.Request, reason string) {
	http.Redirect(w, request, "/admin?oidc="+url.QueryEscape(reason), http.StatusFound)
}

func writeOIDCError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, oidc.ErrProviderNotFound):
		JSONError(w, http.StatusNotFound, "oidc provider not found")
	case errors.Is(err, oidc.ErrProviderDisabled):
		JSONError(w, http.StatusConflict, "oidc provider is disabled")
	case errors.Is(err, oidc.ErrProviderInUse):
		JSONError(w, http.StatusConflict, "oidc provider has linked identities")
	case errors.Is(err, oidc.ErrIssuerImmutable):
		JSONError(w, http.StatusBadRequest, "oidc provider issuer is immutable")
	case errors.Is(err, oidc.ErrInvalidProviderInput):
		JSONError(w, http.StatusBadRequest, "invalid oidc provider configuration")
	case errors.Is(err, oidc.ErrRecentAuthRequired):
		JSONError(w, http.StatusPreconditionRequired, "recent authentication is required")
	case errors.Is(err, oidc.ErrIdentityNotLinked):
		JSONError(w, http.StatusConflict, "oidc identity is not linked")
	case errors.Is(err, oidc.ErrIdentityAlreadyLinked):
		JSONError(w, http.StatusConflict, "oidc identity is already linked")
	case errors.Is(err, oidc.ErrIdentityConflict):
		JSONError(w, http.StatusConflict, "oidc identity is already linked to this user")
	case errors.Is(err, oidc.ErrIdentityNotFound):
		JSONError(w, http.StatusNotFound, "oidc identity not found")
	case errors.Is(err, oidc.ErrCallbackFailed), errors.Is(err, oidc.ErrTransactionInvalid),
		errors.Is(err, oidc.ErrTransactionExpired):
		JSONError(w, http.StatusBadRequest, "oidc transaction is invalid")
	default:
		slog.Error("oidc operation failed", "error", err)
		JSONError(w, http.StatusBadRequest, "oidc request failed")
	}
}

func oidcErrorCategory(err error) string {
	switch {
	case errors.Is(err, oidc.ErrIdentityNotLinked):
		return "identity_not_linked"
	case errors.Is(err, oidc.ErrProviderDisabled):
		return "provider_disabled"
	case errors.Is(err, oidc.ErrTransactionExpired):
		return "transaction_expired"
	default:
		return "generic"
	}
}
