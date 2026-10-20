package metricsfanout

import (
	"net/netip"
	"time"
)

type bucket struct {
	tokens float64
	at     time.Time
}

func (b *bucket) take(now time.Time, burst, rate float64) bool {
	if elapsed := now.Sub(b.at).Seconds(); elapsed > 0 {
		b.tokens += elapsed * rate
		if b.tokens > burst {
			b.tokens = burst
		}
		b.at = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type ipEntry struct {
	bucket   bucket
	reserved int
	touched  time.Time
}

type admission struct {
	s        *Service
	ip       netip.Addr
	released bool
}

func canonicalIP(value string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		address, parseErr := netip.ParseAddrPort(value)
		if parseErr != nil {
			return netip.Addr{}, parseErr
		}
		ip = address.Addr()
	}
	return ip.Unmap().WithZone(""), nil
}

func (s *Service) reserve(value string) (*admission, Reason) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clockNow()
	if s.closing || s.ctx.Err() != nil {
		return nil, ReasonClosed
	}
	if !s.global.take(now, s.limits.globalBurst, s.limits.globalRate) {
		return nil, ReasonRate
	}
	ip, err := canonicalIP(value)
	if err != nil {
		return nil, ReasonInvalidIP
	}
	entry := s.ips[ip]
	if entry == nil {
		s.pruneIPs(now)
		if len(s.ips) >= s.limits.tracked {
			return nil, ReasonIPCapacity
		}
		entry = &ipEntry{bucket: bucket{tokens: s.limits.ipBurst, at: now}}
		s.ips[ip] = entry
	}
	entry.touched = now
	if !entry.bucket.take(now, s.limits.ipBurst, s.limits.ipRate) {
		return nil, ReasonRate
	}
	if s.reserved >= s.limits.global {
		return nil, ReasonGlobalLimit
	}
	if entry.reserved >= s.limits.perIP {
		return nil, ReasonIPLimit
	}
	entry.reserved++
	s.reserved++
	s.wait.Add(1)
	return &admission{s: s, ip: ip}, ReasonNone
}

func (s *Service) pruneIPs(now time.Time) {
	for ip, entry := range s.ips {
		if entry.reserved == 0 && now.Sub(entry.touched) >= ipRetention {
			delete(s.ips, ip)
		}
	}
}

func (a *admission) release() {
	a.s.mu.Lock()
	if a.released {
		a.s.mu.Unlock()
		return
	}
	a.released = true
	a.s.reserved--
	entry := a.s.ips[a.ip]
	entry.reserved--
	entry.touched = a.s.clockNow()
	a.s.mu.Unlock()
	a.s.wait.Done()
}
