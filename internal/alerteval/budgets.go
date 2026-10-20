package alerteval

import (
	"errors"
	"time"
)

const (
	MaximumRules              = 256
	MaximumScopeNodes         = 1000
	MaximumScopeGroups        = 128
	MaximumPairs              = 10000
	MaximumStateRows          = 100000
	MaximumHistoryRows        = 100000
	MaximumRequestBytes       = 32 << 10
	MaximumWindowSeconds      = 86400
	MinimumWindowSeconds      = 10
	MaximumDelaySeconds       = 86400
	DefaultNoDataDelaySeconds = 300
	DefaultErrorDelaySeconds  = 60
	EvaluationInterval        = 30 * time.Second
	MaximumPollGap            = 2 * EvaluationInterval
	RoundBudget               = 25 * time.Second
	BatchSize                 = 256
	QueryTimeout              = 5 * time.Second
	CommitTimeout             = 2 * time.Second
	LeaseDuration             = 15 * time.Second
	MaximumWindowSamples      = 100000
	HistoryRetention          = 30 * 24 * time.Hour
)

var (
	ErrInvalidPolicy      = errors.New("invalid alert evaluation policy")
	ErrInvalidObservation = errors.New("invalid alert evaluation evidence")
	ErrCapacity           = errors.New("alert evaluation capacity exhausted")
	ErrConflict           = errors.New("alert evaluation revision changed")
	ErrPolicyInUse        = errors.New("active incident requires its original numeric policy")
	ErrUnavailable        = errors.New("alert evaluation persistence unavailable")
)
