package alerteval

import (
	"errors"
	"math"
	"testing"
	"time"
)

func completeEvidenceWindow() EvidenceWindow {
	start := time.Unix(1800000000, 0)
	return EvidenceWindow{From: start, To: start.Add(10 * time.Second), Cadence: 5 * time.Second, Aggregation: "avg", Points: []EvidencePoint{
		{At: start, Value: 10, ID: "a"}, {At: start.Add(5 * time.Second), Value: 30, ID: "b"}, {At: start.Add(10 * time.Second), Value: 20, ID: "c"},
	}}
}

func TestWindowGaugeAggregationsAndInputImmutability(t *testing.T) {
	for aggregation, want := range map[string]float64{"avg": 20, "min": 10, "max": 30, "last": 20} {
		window := completeEvidenceWindow()
		window.Aggregation = aggregation
		window.Points[0], window.Points[2] = window.Points[2], window.Points[0]
		got, err := AggregateWindow(window)
		if err != nil || got != want {
			t.Fatalf("%s: got %v/%v, want %v", aggregation, got, err, want)
		}
		if window.Points[0].ID != "c" {
			t.Fatal("window evaluation mutated input order")
		}
	}
}

func TestWindowPredecessorProvesCoverageWithoutBiasingGaugeMean(t *testing.T) {
	window := completeEvidenceWindow()
	window.Points[0].At = window.From.Add(-5 * time.Second)
	window.Points[0].Value = 1000
	value, err := AggregateWindow(window)
	if err != nil || value != 25 {
		t.Fatalf("out-of-window predecessor biased mean: %v/%v", value, err)
	}
	window.Points = append(window.Points[:1], window.Points[2])
	if _, err := AggregateWindow(window); !errors.Is(err, ErrIncompleteWindow) {
		t.Fatal("one in-window gauge observation was accepted")
	}
}

func TestWindowRateClipsBoundaryIntervalsAndRejectsOverflow(t *testing.T) {
	window := completeEvidenceWindow()
	window.Aggregation = "rate"
	window.Points[0].At = window.From.Add(-5 * time.Second)
	window.Points[0].Value, window.Points[1].Value, window.Points[2].Value = 100, 200, 250
	if value, err := AggregateWindow(window); err != nil || value != 10 {
		t.Fatalf("clipped rate: %v/%v", value, err)
	}
	window.Points[0].At = window.From
	window.Points[1].At = window.From.Add(time.Nanosecond)
	window.Points[0].Value, window.Points[1].Value, window.Points[2].Value = 0, math.MaxFloat64, math.MaxFloat64
	if value, err := AggregateWindow(window); err != nil || !finite(value) || value != math.MaxFloat64/10 {
		t.Fatalf("avoidable intermediate overflow: %v/%v", value, err)
	}
	window.To = window.From.Add(time.Nanosecond)
	window.Points = window.Points[:2]
	if _, err := AggregateWindow(window); !errors.Is(err, ErrInvalidObservation) {
		t.Fatal("unrepresentable rate accepted")
	}
}

func TestWindowRejectsInvalidPolicyAndEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*EvidenceWindow){
		"zero start":          func(w *EvidenceWindow) { w.From = time.Time{} },
		"reversed":            func(w *EvidenceWindow) { w.To = w.From },
		"too wide":            func(w *EvidenceWindow) { w.To = w.From.Add((MaximumWindowSeconds + 1) * time.Second) },
		"zero cadence":        func(w *EvidenceWindow) { w.Cadence = 0 },
		"huge cadence":        func(w *EvidenceWindow) { w.Cadence = (MaximumWindowSeconds + 1) * time.Second },
		"unknown aggregation": func(w *EvidenceWindow) { w.Aggregation = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			w := completeEvidenceWindow()
			mutate(&w)
			if _, err := AggregateWindow(w); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("invalid policy: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*EvidenceWindow){
		"missing source":     func(w *EvidenceWindow) { w.Points[1].ID = "" },
		"missing time":       func(w *EvidenceWindow) { w.Points[1].At = time.Time{} },
		"nan":                func(w *EvidenceWindow) { w.Points[1].Value = math.NaN() },
		"infinity":           func(w *EvidenceWindow) { w.Points[1].Value = math.Inf(1) },
		"conflicting source": func(w *EvidenceWindow) { w.Points[1].ID = w.Points[0].ID },
		"ambiguous time":     func(w *EvidenceWindow) { w.Points[1].At = w.Points[0].At },
	} {
		t.Run(name, func(t *testing.T) {
			w := completeEvidenceWindow()
			mutate(&w)
			if _, err := AggregateWindow(w); !errors.Is(err, ErrInvalidObservation) {
				t.Fatalf("invalid observation: %v", err)
			}
		})
	}
}

func TestWindowCapacityAndMissingCoverage(t *testing.T) {
	w := completeEvidenceWindow()
	w.Points = make([]EvidencePoint, MaximumWindowSamples+1)
	if _, err := AggregateWindow(w); !errors.Is(err, ErrCapacity) {
		t.Fatal("unbounded sample input accepted")
	}
	w = completeEvidenceWindow()
	w.Points = nil
	if _, err := AggregateWindow(w); !errors.Is(err, ErrIncompleteWindow) {
		t.Fatal("empty window accepted")
	}
	w = completeEvidenceWindow()
	w.Points = w.Points[1:]
	if _, err := AggregateWindow(w); !errors.Is(err, ErrIncompleteWindow) {
		t.Fatal("missing start coverage accepted")
	}
	w = completeEvidenceWindow()
	w.To = w.From.Add(time.Minute)
	w.Points[1].At = w.From.Add(31 * time.Second)
	w.Points[2].At = w.To
	if _, err := AggregateWindow(w); !errors.Is(err, ErrIncompleteWindow) {
		t.Fatal("gap between sources was filled")
	}
}
