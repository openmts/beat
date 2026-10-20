package metricquery

import (
	"errors"
	"time"
)

const (
	MaximumSeries         = 8
	MinimumPoints         = 2
	MaximumPoints         = 600
	MaximumRange          = 31 * 24 * time.Hour
	QueryTimeout          = 5 * time.Second
	MaximumConcurrent     = 8
	MaximumPerClient      = 2
	MaximumReferenceBytes = 32
	MaximumNodeIDBytes    = 128
	MaximumScannedSamples = 1_000_000
)

var (
	ErrInvalid     = errors.New("invalid metric query")
	ErrCapacity    = errors.New("metric query capacity exceeded")
	ErrTimeout     = errors.New("metric query timed out")
	ErrUnavailable = errors.New("metric storage is unavailable")
	ErrArithmetic  = errors.New("metric aggregation exceeds the finite numeric range")
)

type Request struct {
	From      time.Time       `json:"from"`
	To        time.Time       `json:"to"`
	MaxPoints int             `json:"max_points"`
	Series    []SeriesRequest `json:"series"`
}

type SeriesRequest struct {
	DeviceID    string `json:"device_id,omitempty"`
	RefID       string `json:"ref_id"`
	NodeID      string `json:"node_id"`
	Metric      string `json:"metric"`
	Aggregation string `json:"aggregation"`
}

type Point struct {
	Timestamp int64    `json:"timestamp"`
	Value     *float64 `json:"value"`
}

type Series struct {
	DeviceID      string     `json:"device_id,omitempty"`
	RefID         string     `json:"ref_id"`
	NodeID        string     `json:"node_id"`
	Metric        string     `json:"metric"`
	Label         string     `json:"label"`
	Unit          string     `json:"unit"`
	Aggregation   string     `json:"aggregation"`
	AvailableFrom *time.Time `json:"available_from"`
	State         string     `json:"state"`
	Points        []Point    `json:"points"`
}

type Response struct {
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	IntervalMS int64     `json:"interval_ms"`
	Series     []Series  `json:"series"`
}

type Scan struct {
	DeviceID, Vendor            string
	NodeID, Metric, Aggregation string
	From, To                    time.Time
	Interval                    time.Duration
	Buckets                     int
}

type Values struct {
	AvailableFrom *time.Time
	Points        []Point
}

type Access struct {
	PublicOnly bool
	ClientKey  string
}
