package runtimediag

import (
	"os"
	"time"
)

// admitLocked 实施“一个定时捕获”“每所有者每分钟两个瞬时捕获”和总容量上限。
func (service *Service) admitLocked(ownerID, kind string, now time.Time) error {
	if service.cleanupFailures > 0 {
		return Error{Code: CodeCapacity}
	}
	if KindIsTimed(kind) && service.timedLocked() {
		return Error{Code: CodeConflict}
	}
	if !KindIsTimed(kind) {
		recent := service.recentInstantaneousLocked(ownerID, now)
		if len(recent) >= instantaneousPerMinute {
			return Error{Code: CodeRateLimited}
		}
		service.rates[ownerID] = append(recent, now)
	}
	if service.active >= 4 || service.directoryBytesLocked(now) >= service.limits.MaximumDirectoryBytes {
		return Error{Code: CodeCapacity}
	}
	return nil
}

func (service *Service) timedLocked() bool {
	for _, item := range service.items {
		if item.Status == StatusRunning && KindIsTimed(item.Kind) {
			return true
		}
	}
	return false
}

func (service *Service) recentInstantaneousLocked(ownerID string, now time.Time) []time.Time {
	recent := make([]time.Time, 0, instantaneousPerMinute)
	for _, at := range service.rates[ownerID] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	return recent
}

// directoryBytesLocked 统计私有目录当前占用，用于总容量上限。
func (service *Service) directoryBytesLocked(time.Time) int64 {
	entries, err := os.ReadDir(service.config.Directory)
	if err != nil {
		return service.limits.MaximumDirectoryBytes
	}
	var total int64
	for _, item := range entries {
		info, err := item.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}
