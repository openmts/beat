package agentrolloutremote

import (
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("GitHub release source configuration or request is invalid")
	ErrNotFound    = errors.New("GitHub release was not found")
	ErrRateLimited = errors.New("GitHub release source is rate limited")
	ErrUnavailable = errors.New("GitHub release source is unavailable")
)

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }
