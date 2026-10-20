package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/alerteval"
)

func TestAlertTrafficUsesNanosecondsAndFencesReceiptRace(t *testing.T) {
	metrics := metricQueryMTS(t)
	at := time.Now().UTC()
	input := ingestInput(at, 100)
	acceptIngest(t, metrics, input)
	request := AlertTrafficRequest{NodeID: "node", ReceiptID: input.Report.SampleID, Start: at.Add(-time.Second), End: at.Add(time.Nanosecond)}
	totals, err := metrics.ReadAlertTraffic(t.Context(), request)
	if err != nil || totals.TrackedSince == nil || !totals.TrackedSince.Equal(at) {
		t.Fatal(totals, err)
	}
	request.ReceiptID = "stale"
	if _, err := metrics.ReadAlertTraffic(t.Context(), request); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.ReadAlertTraffic(ctx, request); err == nil {
		t.Fatal("cancellation ignored")
	}
	lock := metrics.nodeLock("node")
	lock.Lock()
	waiting, stop := context.WithTimeout(t.Context(), time.Millisecond)
	defer stop()
	if _, err := metrics.ReadAlertTraffic(waiting, request); err == nil {
		t.Fatal("lock deadline ignored")
	}
	lock.Unlock()
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.ReadAlertTraffic(t.Context(), request); err == nil {
		t.Fatal("closed store ignored")
	}
}

func TestAlertTrafficRejectsNegativeAndOverflow(t *testing.T) {
	for _, scenario := range []string{"negative", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			metrics := metricQueryMTS(t)
			at := time.Now().UTC()
			input := ingestInput(at, 100)
			acceptIngest(t, metrics, input)
			value := mts.Float64Value(-1)
			if scenario == "overflow" {
				value = mts.Float64Value(math.MaxFloat64)
			}
			points := []mts.Point{
				{Measurement: trafficReceivedDelta, Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": value}, Timestamp: at.Add(-time.Second).UnixNano()},
				{Measurement: trafficReceivedDelta, Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": value}, Timestamp: at.Add(-2 * time.Second).UnixNano()},
			}
			if err := metrics.ImportPoints(t.Context(), points); err != nil {
				t.Fatal(err)
			}
			_, err := metrics.ReadAlertTraffic(t.Context(), AlertTrafficRequest{NodeID: "node", ReceiptID: input.Report.SampleID, Start: at.Add(-3 * time.Second), End: at.Add(time.Nanosecond)})
			if err == nil {
				t.Fatal("invalid traffic data accepted")
			}
		})
	}
}
