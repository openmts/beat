package geoip

import "errors"

var (
	ErrUnavailable = errors.New("geoip unavailable")
	ErrInvalid     = errors.New("invalid geoip request")
	ErrForbidden   = errors.New("geoip permission denied")
	ErrNotFound    = errors.New("node not found")
	ErrConflict    = errors.New("geoip revision conflict")
)
