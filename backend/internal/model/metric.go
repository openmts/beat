package model

import "time"

type NodeMetric struct {
	NodeID    string
	Timestamp time.Time
	CPU       float64
	Memory    float64
	Disk      float64
	NetIn     float64
	NetOut    float64
	Load1     float64
	Load5     float64
	Load15    float64
	Uptime    float64
}
