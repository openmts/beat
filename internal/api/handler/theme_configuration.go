package handler

import (
	"io"
	"net/http"
)

func (handler *ThemeHandler) HandlePreview(w http.ResponseWriter, request *http.Request) {
	descriptor, err := handler.service.DescribePackage(request.Context(), request.PathValue("id"))
	if err != nil {
		writeThemeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	JSONResponse(w, http.StatusOK, descriptor)
}

func (handler *ThemeHandler) HandleBuiltIn(w http.ResponseWriter, request *http.Request) {
	ctx, actorID, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	if err := handler.service.Fallback(ctx, actorID, "owner_recovery"); err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, map[string]string{"active_theme": "built-in"})
}

func (handler *ThemeHandler) HandleConfiguration(w http.ResponseWriter, request *http.Request) {
	ctx, _, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, 16<<10)
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "theme configuration is too large or unreadable")
		return
	}
	descriptor, err := handler.service.Configure(ctx, request.PathValue("id"), raw)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, descriptor)
}
