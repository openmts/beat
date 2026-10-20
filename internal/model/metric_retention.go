package model

import "time"

type MetricRetentionJob struct {
	Key           string
	Revision      int64
	RetentionDays int
	Attempts      int
	StartedAt     time.Time
}

type MetricRetentionCompletion struct {
	Job         MetricRetentionJob
	Error       string
	CompletedAt time.Time
}
