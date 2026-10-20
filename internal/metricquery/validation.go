package metricquery

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
)

func ValidateRequest(request Request) (time.Duration, int, error) {
	if err := validateTimeRange(request); err != nil {
		return 0, 0, err
	}
	if request.MaxPoints < MinimumPoints || request.MaxPoints > MaximumPoints {
		return 0, 0, fmt.Errorf("%w: max_points must be between 2 and 600", ErrInvalid)
	}
	if len(request.Series) == 0 || len(request.Series) > MaximumSeries {
		return 0, 0, fmt.Errorf("%w: one to eight series are required", ErrInvalid)
	}
	if err := validateSeriesRequests(request.Series); err != nil {
		return 0, 0, err
	}
	durationMS := request.To.Sub(request.From).Milliseconds()
	intervalMS := (durationMS + int64(request.MaxPoints) - 1) / int64(request.MaxPoints)
	count := (durationMS + intervalMS - 1) / intervalMS
	return time.Duration(intervalMS) * time.Millisecond, int(count), nil
}

func validateTimeRange(request Request) error {
	if request.From.Before(time.Unix(0, 0)) || request.To.After(time.Unix(0, math.MaxInt64)) {
		return fmt.Errorf("%w: timestamp is outside supported storage range", ErrInvalid)
	}
	duration := request.To.Sub(request.From)
	if duration < time.Millisecond || duration > MaximumRange {
		return fmt.Errorf("%w: range must be between one millisecond and 31 days", ErrInvalid)
	}
	if request.From.Nanosecond()%int(time.Millisecond) != 0 || request.To.Nanosecond()%int(time.Millisecond) != 0 {
		return fmt.Errorf("%w: timestamps require millisecond precision", ErrInvalid)
	}
	return nil
}

func validateSeriesRequests(series []SeriesRequest) error {
	seen := make(map[string]bool, len(series))
	for _, selection := range series {
		if !validIdentifier(selection.RefID, MaximumReferenceBytes) || seen[selection.RefID] {
			return fmt.Errorf("%w: series references must be unique and bounded", ErrInvalid)
		}
		if !validIdentifier(selection.NodeID, MaximumNodeIDBytes) || !metriccatalog.ValidKey(selection.Metric) {
			return fmt.Errorf("%w: invalid node or metric key", ErrInvalid)
		}
		if !validSeriesDevice(selection) {
			return ErrInvalid
		}
		if !slices.Contains([]string{"", "avg", "min", "max", "sum", "last", "rate"}, selection.Aggregation) {
			return fmt.Errorf("%w: unsupported aggregation", ErrInvalid)
		}
		seen[selection.RefID] = true
	}
	return nil
}

func validIdentifier(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if !validIdentifierCharacter(character) {
			return false
		}
	}
	return true
}

func validIdentifierCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-'
}
