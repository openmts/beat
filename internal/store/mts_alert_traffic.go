package store

import (
	"context"
	"math"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

type AlertTrafficRequest struct {
	NodeID, ReceiptID string
	Start, End        time.Time
}

func (s *MTSStore) ReadAlertTraffic(ctx context.Context, request AlertTrafficRequest) (model.TrafficTotals, error) {
	if request.NodeID == "" || request.ReceiptID == "" || !request.End.After(request.Start) {
		return model.TrafficTotals{}, alerteval.ErrInvalidPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, alerteval.QueryTimeout)
	defer cancel()
	unlock, err := s.lockMetricOperation(ctx, false)
	if err != nil {
		return model.TrafficTotals{}, err
	}
	defer unlock()
	release, err := s.lockAgentNode(ctx, request.NodeID)
	if err != nil {
		return model.TrafficTotals{}, err
	}
	defer release()
	receipt, err := s.readAgentReceipt(ctx, agentReceiptQuery{nodeID: request.NodeID, end: math.MaxInt64})
	if err != nil {
		return model.TrafficTotals{}, err
	}
	if receipt == nil || receipt.SampleID != request.ReceiptID {
		return model.TrafficTotals{}, alerteval.ErrConflict
	}
	if request.End.After(receipt.CapturedAt.Add(time.Nanosecond)) {
		return model.TrafficTotals{}, alerteval.ErrInvalidPolicy
	}
	return s.readAlertTrafficLocked(ctx, request)
}

func (s *MTSStore) readAlertTrafficLocked(ctx context.Context, request AlertTrafficRequest) (model.TrafficTotals, error) {
	totals := model.TrafficTotals{}
	remaining := alerteval.MaximumWindowSamples
	for _, key := range []string{trafficReceivedDelta, trafficSentDelta} {
		points, err := s.queryRawMetric(ctx, rawMetricSelection{nodeID: request.NodeID, metric: key, start: request.Start, end: request.End, limit: remaining})
		if err != nil {
			return totals, err
		}
		remaining -= len(points)
		total := 0.0
		for _, point := range points {
			if point.Value < 0 {
				return totals, alerteval.ErrInvalidObservation
			}
			total += point.Value
		}
		if math.IsInf(total, 0) {
			return totals, alerteval.ErrInvalidObservation
		}
		if key == trafficReceivedDelta {
			totals.Received = total
			if len(points) > 0 {
				at := points[0].Timestamp
				totals.TrackedSince = &at
			}
		} else {
			totals.Sent = total
		}
	}
	return totals, ctx.Err()
}
