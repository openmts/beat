package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxCustomMetricRequestBytes = 256 << 10

func parseCustomMetricRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxCustomMetricRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return customMetricDecodeError(w, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return customMetricDecodeError(w, err)
	}
	return true
}

func customMetricDecodeError(w http.ResponseWriter, err error) bool {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		JSONError(w, http.StatusRequestEntityTooLarge, "custom metric request exceeds 256 KiB")
		return false
	}
	JSONError(w, http.StatusBadRequest, "invalid custom metric request body")
	return false
}
