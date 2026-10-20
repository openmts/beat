package alerteval

import (
	"errors"
	"testing"
	"time"
)

func TestWindowRequiresCompleteNewSourcesAndIgnoresRepeatedCustomExecution(t *testing.T) {
	start := time.Unix(1800000000, 0)
	points := []EvidencePoint{{At: start, Value: 10, ID: "first"}, {At: start.Add(10 * time.Second), Value: 30, ID: "second"}, {At: start.Add(20 * time.Second), Value: 30, ID: "second"}}
	query := EvidenceWindow{From: start, To: start.Add(10 * time.Second), Cadence: 5 * time.Second, Aggregation: "avg", Points: points}
	value, err := AggregateWindow(query)
	if err != nil || value != 20 {
		t.Fatal("duplicate execution biased window", value, err)
	}
	query.To = start.Add(time.Minute)
	if _, err := AggregateWindow(query); !errors.Is(err, ErrIncompleteWindow) {
		t.Fatal("partial window produced alert value", err)
	}
	query.Points = []EvidencePoint{{At: start, Value: 10, ID: "same"}, {At: query.To, Value: 10, ID: "same"}}
	if _, err := AggregateWindow(query); !errors.Is(err, ErrIncompleteWindow) {
		t.Fatal("duplicate source invented window coverage", err)
	}
}

func TestWindowCounterRateHandlesResetWithoutFabricatingMissingData(t *testing.T) {
	start := time.Unix(1800000000, 0)
	query := EvidenceWindow{From: start, To: start.Add(10 * time.Second), Cadence: 5 * time.Second, Aggregation: "rate", Points: []EvidencePoint{
		{At: start, Value: 100, ID: "a"}, {At: start.Add(5 * time.Second), Value: 150, ID: "b"}, {At: start.Add(10 * time.Second), Value: 20, ID: "c"}}}
	value, err := AggregateWindow(query)
	if err != nil || value != 7 {
		t.Fatal("counter reset rate", value, err)
	}
	query.Points[1].Value = -1
	if _, err := AggregateWindow(query); !errors.Is(err, ErrInvalidObservation) {
		t.Fatal("negative counter accepted", err)
	}
}
