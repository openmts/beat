package alerteval

import (
	"errors"
	"slices"
	"sort"
	"time"
)

var ErrIncompleteWindow = errors.New("alert window has incomplete source coverage")

type EvidencePoint struct {
	At    time.Time
	Value float64
	ID    string
}

type EvidenceWindow struct {
	From, To    time.Time
	Cadence     time.Duration
	Aggregation string
	Points      []EvidencePoint
}

// AggregateWindow 只计算有完整边界和连续采样证据的窗口，不填补缺失值。
func AggregateWindow(window EvidenceWindow) (float64, error) {
	if err := validateEvidenceWindow(window); err != nil {
		return 0, err
	}
	points, err := distinctWindowPoints(window.Points)
	if err != nil {
		return 0, err
	}
	points, err = coveredWindowPoints(window, points)
	if err != nil {
		return 0, err
	}
	if window.Aggregation == "rate" {
		return windowCounterRate(window, points)
	}
	if points[0].At.Before(window.From) {
		points = points[1:]
	}
	if len(points) < 2 {
		return 0, ErrIncompleteWindow
	}
	return windowGauge(window.Aggregation, points)
}

func validateEvidenceWindow(window EvidenceWindow) error {
	if len(window.Points) > MaximumWindowSamples {
		return ErrCapacity
	}
	if window.From.IsZero() || !window.To.After(window.From) || window.To.Sub(window.From) > MaximumWindowSeconds*time.Second {
		return ErrInvalidPolicy
	}
	if window.Cadence <= 0 || window.Cadence > MaximumWindowSeconds*time.Second {
		return ErrInvalidPolicy
	}
	if !slices.Contains([]string{"last", "avg", "min", "max", "sum", "rate"}, window.Aggregation) {
		return ErrInvalidPolicy
	}
	return nil
}

func distinctWindowPoints(input []EvidencePoint) ([]EvidencePoint, error) {
	points := slices.Clone(input)
	for _, point := range points {
		if !validEvidencePoint(point) {
			return nil, ErrInvalidObservation
		}
	}
	slices.SortStableFunc(points, func(left, right EvidencePoint) int { return left.At.Compare(right.At) })
	seen := make(map[string]float64, len(points))
	result := points[:0]
	for _, point := range points {
		if previous, exists := seen[point.ID]; exists {
			if previous != point.Value {
				return nil, ErrInvalidObservation
			}
			continue
		}
		if len(result) > 0 && result[len(result)-1].At.Equal(point.At) {
			return nil, ErrInvalidObservation
		}
		seen[point.ID] = point.Value
		result = append(result, point)
	}
	return result, nil
}

func validEvidencePoint(point EvidencePoint) bool {
	return point.ID != "" && !point.At.IsZero() && finite(point.Value)
}

func coveredWindowPoints(window EvidenceWindow, points []EvidencePoint) ([]EvidencePoint, error) {
	start := sort.Search(len(points), func(index int) bool { return points[index].At.After(window.From) }) - 1
	end := sort.Search(len(points), func(index int) bool { return points[index].At.After(window.To) })
	if start < 0 || end-start < 2 {
		return nil, ErrIncompleteWindow
	}
	selected := points[start:end]
	if !selected[len(selected)-1].At.Equal(window.To) {
		return nil, ErrIncompleteWindow
	}
	maximumGap := max(2*window.Cadence, 30*time.Second)
	for index := 1; index < len(selected); index++ {
		gap := selected[index].At.Sub(selected[index-1].At)
		if gap <= 0 {
			return nil, ErrInvalidObservation
		}
		if gap > maximumGap {
			return nil, ErrIncompleteWindow
		}
	}
	return selected, nil
}
