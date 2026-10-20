package metricsfanout

import (
	"testing"
	"time"
)

func TestCanonicalIPLimitsAndBoundedAddressState(t *testing.T) {
	s, clock := fixture(t, nil)
	s.limits = limits{global: 2, perIP: 1, tracked: 2, globalBurst: 100, globalRate: 100, ipBurst: 100, ipRate: 100}
	s.global.tokens = 100
	first, reason := s.reserve("192.0.2.1:1234")
	if reason != ReasonNone {
		t.Fatal(reason)
	}
	if _, reason := s.reserve("::ffff:192.0.2.1"); reason != ReasonIPLimit {
		t.Fatal("mapped address split limit", reason)
	}
	second, reason := s.reserve("[2001:db8::1]:1234")
	if reason != ReasonNone {
		t.Fatal(reason)
	}
	if _, reason := s.reserve("2001:db8::1"); reason != ReasonGlobalLimit {
		t.Fatal(reason)
	}
	if _, reason := s.reserve("192.0.2.3"); reason != ReasonIPCapacity {
		t.Fatal(reason)
	}
	first.release()
	first.release()
	second.release()
	clock.advance(ipRetention)
	next, reason := s.reserve("192.0.2.3")
	if reason != ReasonNone {
		t.Fatal(reason)
	}
	next.release()
	if s.Stats().TrackedIPs > 2 || s.Stats().Reserved != 0 {
		t.Fatal(s.Stats())
	}
	if _, reason := s.reserve("untrusted-header"); reason != ReasonInvalidIP {
		t.Fatal(reason)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, reason := s.reserve("192.0.2.1"); reason != ReasonClosed {
		t.Fatal(reason)
	}
}

func TestHandshakeBucketsIncludeRejectedAttemptsAndResistClockRollback(t *testing.T) {
	s, clock := fixture(t, nil)
	s.limits.globalBurst = 2
	s.limits.globalRate = 1
	s.limits.ipBurst = 1
	s.limits.ipRate = 1
	s.global.tokens = 2
	ticket, reason := s.reserve("127.0.0.1")
	if reason != ReasonNone {
		t.Fatal(reason)
	}
	ticket.release()
	if _, reason := s.reserve("127.0.0.1"); reason != ReasonRate {
		t.Fatal(reason)
	}
	if _, reason := s.reserve("127.0.0.2"); reason != ReasonRate {
		t.Fatal(reason)
	}
	clock.rollback(time.Hour)
	if _, reason := s.reserve("127.0.0.1"); reason != ReasonRate {
		t.Fatal("wall clock changed bucket", reason)
	}
	clock.advance(time.Second)
	ticket, reason = s.reserve("127.0.0.1")
	if reason != ReasonNone {
		t.Fatal(reason)
	}
	ticket.release()
	var bucket bucket
	if bucket.take(time.Time{}, 1, 1) {
		t.Fatal("empty bucket admitted")
	}
}
