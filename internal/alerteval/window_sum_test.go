package alerteval

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestWindowSumSupportsCatalogDeltaAndCustomAggregations(t *testing.T) {
	at := time.Now()
	window := EvidenceWindow{From: at, To: at.Add(10 * time.Second), Cadence: 5 * time.Second, Aggregation: "sum", Points: []EvidencePoint{{At: at.Add(-time.Second), Value: 100, ID: "baseline"}, {At: at.Add(5 * time.Second), Value: 3, ID: "one"}, {At: at.Add(10 * time.Second), Value: 4, ID: "two"}}}
	value, err := AggregateWindow(window)
	if err != nil || value != 7 {
		t.Fatal(value, err)
	}
	window.Points[1].Value = math.MaxFloat64
	window.Points[2].Value = math.MaxFloat64
	if _, err := AggregateWindow(window); !errors.Is(err, ErrInvalidObservation) {
		t.Fatal("sum overflow accepted", err)
	}
}
