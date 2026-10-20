package handler

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"

	"github.com/beat/backend/internal/remoteoperations"
)

// JSON base64 输出最大为 1 MiB*4/3，另为元数据留出明确余量。
const maximumOperationBodyBytes = 2 * 1024 * 1024

func decodeOperation(r *http.Request, target any) error {
	if r.Body == nil {
		return remoteoperations.ErrInvalid
	}
	reader := io.Reader(io.LimitReader(r.Body, maximumOperationBodyBytes+1))
	switch r.Header.Get("Content-Encoding") {
	case "":
	case "gzip":
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return remoteoperations.ErrInvalid
		}
		defer func() { _ = compressed.Close() }()
		reader = compressed
	default:
		return remoteoperations.ErrInvalid
	}
	bounded := &io.LimitedReader{R: reader, N: maximumOperationBodyBytes + 1}
	decoder := json.NewDecoder(bounded)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return remoteoperations.ErrInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || bounded.N <= 0 {
		return remoteoperations.ErrInvalid
	}
	return nil
}
