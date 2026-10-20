package metricquery

import (
	"math"
	"slices"
	"time"
)

type bucketValue struct {
	count           int64
	value, duration float64
}

type observation struct {
	at    time.Time
	value float64
}

type Buckets struct {
	plan     Scan
	values   []bucketValue
	previous *observation
	err      error
}

func NewBuckets(plan Scan) *Buckets {
	buckets := &Buckets{plan: plan}
	if plan.Buckets < 1 || plan.Buckets > MaximumPoints || plan.Interval < time.Millisecond || !plan.To.After(plan.From) {
		buckets.err = ErrInvalid
		return buckets
	}
	if !slices.Contains([]string{"avg", "min", "max", "sum", "last", "rate"}, plan.Aggregation) {
		buckets.err = ErrInvalid
		return buckets
	}
	buckets.values = make([]bucketValue, plan.Buckets)
	return buckets
}

func (buckets *Buckets) Add(timestamp time.Time, value float64) error {
	if buckets.err != nil {
		return buckets.err
	}
	if !finite(value) {
		buckets.err = ErrArithmetic
		return buckets.err
	}
	if !timestamp.Before(buckets.plan.To) {
		return nil
	}
	if buckets.plan.Aggregation == "rate" {
		buckets.err = buckets.addRate(observation{at: timestamp, value: value})
		return buckets.err
	}
	if timestamp.Before(buckets.plan.From) {
		return nil
	}
	index := int(timestamp.Sub(buckets.plan.From) / buckets.plan.Interval)
	if index >= len(buckets.values) {
		buckets.err = ErrInvalid
		return buckets.err
	}
	buckets.err = buckets.values[index].add(buckets.plan.Aggregation, value)
	return buckets.err
}

func (value *bucketValue) add(aggregation string, sample float64) error {
	value.count++
	if value.count == 1 {
		value.value = sample
		return nil
	}
	switch aggregation {
	case "avg":
		value.value = value.value*(float64(value.count-1)/float64(value.count)) + sample/float64(value.count)
	case "min":
		value.value = min(value.value, sample)
	case "max":
		value.value = max(value.value, sample)
	case "sum":
		value.value += sample
	case "last":
		value.value = sample
	}
	if !finite(value.value) {
		return ErrArithmetic
	}
	return nil
}

func (buckets *Buckets) addRate(current observation) error {
	previous := buckets.previous
	buckets.previous = &current
	if current.value < 0 {
		return ErrArithmetic
	}
	if previous == nil || current.at.Before(buckets.plan.From) {
		return nil
	}
	if !current.at.After(previous.at) {
		return ErrArithmetic
	}
	delta := current.value - previous.value
	if delta < 0 {
		delta = current.value
	}
	index := int(current.at.Sub(buckets.plan.From) / buckets.plan.Interval)
	if index >= len(buckets.values) {
		return ErrInvalid
	}
	bucket := &buckets.values[index]
	bucket.count++
	bucket.value += delta
	bucket.duration += current.at.Sub(previous.at).Seconds()
	if !finite(bucket.value) {
		return ErrArithmetic
	}
	return nil
}

func (buckets *Buckets) Points() ([]Point, error) {
	if buckets.err != nil {
		return nil, buckets.err
	}
	points := make([]Point, len(buckets.values))
	for index, bucket := range buckets.values {
		points[index].Timestamp = buckets.plan.From.Add(time.Duration(index) * buckets.plan.Interval).UnixMilli()
		if bucket.count == 0 {
			continue
		}
		value := bucket.value
		if buckets.plan.Aggregation == "rate" {
			value /= bucket.duration
		}
		if !finite(value) {
			return nil, ErrArithmetic
		}
		points[index].Value = &value
	}
	return points, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
