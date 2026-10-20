package agentoperations

import (
	"context"
	"errors"
)

type Status struct {
	Enabled   bool   `json:"enabled"`
	Ready     bool   `json:"ready"`
	Active    int    `json:"active"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (runner *Runner) Status() Status {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return Status{Enabled: runner.settings.config.Enabled, Ready: runner.lastError == "", Active: len(runner.active), ErrorCode: runner.lastError}
}

type callbackError string

func (err callbackError) Error() string { return string(err) }

func ErrorCode(err error) string {
	if encoded, ok := err.(callbackError); ok {
		return string(encoded)
	}
	if err == nil {
		return ""
	}
	codes := []struct {
		source error
		code   string
	}{
		{ErrPolicy, "local_policy"}, {ErrStorage, "storage_failure"}, {ErrBoundary, "sandbox_unavailable"},
		{ErrTransport, "transport_unavailable"}, {ErrLeaseRejected, "lease_rejected"},
		{ErrFileIntegrity, "file_integrity"}, {ErrSourceChanged, "source_changed"}, {ErrFileConflict, "transfer_conflict"},
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"},
	}
	for _, entry := range codes {
		if errors.Is(err, entry.source) {
			return entry.code
		}
	}
	return "operation_failed"
}

func (runner *Runner) observe(err error) {
	code := ErrorCode(err)
	runner.mu.Lock()
	changed := runner.lastError != code
	runner.lastError = code
	runner.mu.Unlock()
	if err != nil && changed && runner.onError != nil {
		runner.onError(callbackError(code))
	}
}
