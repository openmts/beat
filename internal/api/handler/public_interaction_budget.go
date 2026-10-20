package handler

import (
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

// publicInteractionWindow 是匿名来源的固定计数窗口。
const (
	publicInteractionWindow      = time.Minute
	publicInteractionBudgetSlots = 4096
)

// publicInteractionRequestCeiling 是单一来源每分钟可接受的交互次数上限。
const publicInteractionRequestCeiling = model.MaximumPublicInteractionRequestsPerMinute

type publicInteractionSlot struct {
	windowStart time.Time
	count       int
}

// publicInteractionBudget 是有界的匿名限流键；超过容量时对新来源一律拒绝。
type publicInteractionBudget struct {
	mu      sync.Mutex
	slots   map[string]publicInteractionSlot
	maximum int
}

func newPublicInteractionBudget(maximum int) *publicInteractionBudget {
	if maximum < 64 {
		maximum = 64
	}
	return &publicInteractionBudget{slots: make(map[string]publicInteractionSlot), maximum: maximum}
}

// Allow 在窗口内为来源记一次额度；容量耗尽或超限时返回 false。
func (budget *publicInteractionBudget) Allow(key string, now time.Time) bool {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.prune(now)
	if _, tracked := budget.slots[key]; !tracked && len(budget.slots) >= budget.maximum {
		return false
	}
	slot := budget.slots[key]
	if slot.windowStart.IsZero() || now.Sub(slot.windowStart) >= publicInteractionWindow {
		slot = publicInteractionSlot{windowStart: now}
	}
	if slot.count >= publicInteractionRequestCeiling {
		budget.slots[key] = slot
		return false
	}
	slot.count++
	budget.slots[key] = slot
	return true
}

func (budget *publicInteractionBudget) prune(now time.Time) {
	for key, slot := range budget.slots {
		if now.Sub(slot.windowStart) >= publicInteractionWindow {
			delete(budget.slots, key)
		}
	}
}
