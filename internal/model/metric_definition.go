package model

import "time"

type MetricDefinition struct {
	Key                string    `json:"key"`
	Label              string    `json:"label"`
	Description        string    `json:"description"`
	Source             string    `json:"source"`
	Kind               string    `json:"kind"`
	Unit               string    `json:"unit"`
	Aggregations       []string  `json:"aggregations"`
	DefaultAggregation string    `json:"default_aggregation"`
	Public             bool      `json:"public"`
	Enabled            bool      `json:"enabled"`
	RetentionDays      int       `json:"retention_days"`
	Revision           int64     `json:"revision"`
	Minimum            *float64  `json:"-"`
	Maximum            *float64  `json:"-"`
	Tags               []string  `json:"-"`
	RetentionOverride  *int      `json:"retention_override_days"`
	CreatedAt          time.Time `json:"-"`
	UpdatedAt          time.Time `json:"-"`
}

type MetricCatalog struct {
	Version  int                `json:"version"`
	Revision int64              `json:"revision"`
	Metrics  []MetricDefinition `json:"metrics"`
}

type MetricPolicyUpdate struct {
	Revision           int64  `json:"revision"`
	Label              string `json:"label"`
	Description        string `json:"description"`
	DefaultAggregation string `json:"default_aggregation"`
	Public             bool   `json:"public"`
	Enabled            bool   `json:"enabled"`
	RetentionDays      *int   `json:"retention_days"`
}
