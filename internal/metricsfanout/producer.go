package metricsfanout

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

// Run 是唯一producer入口；连接与Stats调用不能启动构建。
func (s *Service) Run(parent context.Context) error {
	if err := s.beginRun(); err != nil {
		return err
	}
	defer s.endRun()
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	ticker := time.NewTicker(Cadence)
	defer ticker.Stop()
	ticks := ticker.C
	if s.ticks != nil {
		ticks = s.ticks
	}
	_, err := s.build(ctx)
	if err != nil {
		return err
	}
	lastEnd := s.clockNow()
	for {
		select {
		case <-ctx.Done():
			if s.observerFailed.Load() {
				return ErrObserverPanic
			}
			return nil
		case tick, ok := <-ticks:
			if !ok {
				return ErrClosed
			}
			if !tick.After(lastEnd) {
				continue
			}
			_, err = s.build(ctx)
			if err != nil {
				return err
			}
			lastEnd = s.clockNow()
		}
	}
}

func (s *Service) build(parent context.Context) (time.Time, error) {
	if parent.Err() != nil {
		return s.config.Now(), nil
	}
	start := s.config.Now()
	monotonicStart := s.clockNow()
	ctx, cancel := context.WithTimeout(parent, BuildTimeout)
	value, reason, err := s.construct(ctx)
	cancel()
	end := s.config.Now()
	if parent.Err() != nil {
		return end, nil
	}
	duration := s.clockNow().Sub(monotonicStart)
	if duration < 0 {
		duration = 0
	}
	s.recordBuild(buildRecord{start, end, duration, reason})
	if reason == ReasonNone {
		s.publish(value)
	}
	s.emit(Event{Kind: EventBuild, Reason: reason, Count: 1, Duration: duration})
	if duration >= Cadence {
		s.emit(Event{Kind: EventLag, Count: int64(duration / Cadence), Duration: duration - Cadence})
	}
	if s.observerFailed.Load() {
		return end, ErrObserverPanic
	}
	return end, err
}

func (s *Service) construct(ctx context.Context) (output *generation, reason Reason, resultErr error) {
	defer func() {
		if recover() != nil {
			output = nil
			reason = ReasonPanic
			resultErr = ErrBuilderPanic
		}
	}()
	snapshot, err := s.config.Build(ctx)
	if err != nil || ctx.Err() != nil {
		return nil, ReasonBuilder, nil
	}
	if !s.validTimestamp(snapshot.Timestamp) || snapshot.Value == nil {
		return nil, ReasonSnapshot, nil
	}
	payload, err := json.Marshal(snapshot.Value)
	if err != nil {
		return nil, ReasonEncoding, nil
	}
	if ctx.Err() != nil {
		return nil, ReasonBuilder, nil
	}
	if len(payload) > MaximumSnapshotBytes {
		return nil, ReasonOversize, nil
	}
	if bytes.Equal(payload, []byte("null")) {
		return nil, ReasonSnapshot, nil
	}
	frame := serverFrame(1, payload)
	return &generation{at: snapshot.Timestamp, payload: frame[len(frame)-len(payload):], frame: frame}, ReasonNone, nil
}

type buildRecord struct {
	start, end time.Time
	duration   time.Duration
	reason     Reason
}

func (s *Service) recordBuild(record buildRecord) {
	start, end, duration, reason := record.start, record.end, record.duration, record.reason
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.LastAttempt = start
	s.stats.BuildDuration = duration
	s.stats.Builds++
	s.stats.SchedulerLag = 0
	if duration >= Cadence {
		s.stats.SkippedCadences += uint64(duration / Cadence)
		s.stats.SchedulerLag = duration - Cadence
	}
	if reason == ReasonNone {
		s.stats.LastSuccess = end
		s.stats.ConsecutiveFailures = 0
		s.stats.Failure = ReasonNone
	} else {
		s.stats.LastFailure = end
		s.stats.Failures++
		s.stats.ConsecutiveFailures++
		s.stats.Failure = reason
	}
}

func (s *Service) publish(value *generation) {
	s.mu.Lock()
	if s.closing || s.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	value.storedAt = s.clockNow()
	value.initialAge = s.effectiveNowLocked().Sub(value.at)
	if value.initialAge < 0 {
		value.initialAge = 0
	}
	value.number = 1
	if s.latest != nil {
		value.number = s.latest.number + 1
	}
	s.latest = value
	dropped := int64(0)
	for c := range s.clients {
		if c.enqueue(value) {
			dropped++
		}
	}
	s.stats.Dropped += uint64(dropped)
	s.mu.Unlock()
	if dropped > 0 {
		s.emit(Event{Kind: EventDrop, Reason: ReasonCoalesced, Count: dropped})
	}
}

func (s *Service) validTimestamp(at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.effectiveNowLocked()
	if at.IsZero() || at.After(s.config.Now().Add(time.Second)) || now.Sub(at) > StaleWindow {
		return false
	}
	return s.latest == nil || at.After(s.latest.at)
}

func (s *Service) beginRun() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.ctx.Err() != nil {
		return ErrClosed
	}
	if s.running {
		return ErrRunning
	}
	s.running = true
	s.wait.Add(1)
	return nil
}

func (s *Service) endRun() { s.mu.Lock(); s.running = false; s.mu.Unlock(); s.wait.Done() }
