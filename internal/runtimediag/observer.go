package runtimediag

import "errors"

// 事件结果使用固定枚举，供指标与日志消费；未知取值一律丢弃，避免高基数标签。
const (
	EventStarted    = "started"
	EventCompleted  = "completed"
	EventFailed     = "failed"
	EventCancelled  = "cancelled"
	EventExpired    = "expired"
	EventDownloaded = "downloaded"
	EventRejected   = "rejected"
)

// Event 是捕获生命周期的有界通知；它不含 profile 内容、路径、请求头或账号数据。
type Event struct {
	Kind   string
	Result string
	Code   string
	Bytes  int64
}

// Observer 接收生命周期事件；实现必须是并发安全且非阻塞的。
type Observer func(Event)

// Statistics 是抓取时读取的当前占用快照。
type Statistics struct {
	Active          int
	Bytes           int64
	CleanupFailures uint64
}

func (service *Service) observe(event Event) {
	if observer := service.observer.Load(); observer != nil {
		(*observer)(event)
	}
}

// SetObserver 在启动期注入指标/日志订阅者；事件读取是无锁的原子指针。
func (service *Service) SetObserver(observer Observer) {
	if observer == nil {
		service.observer.Store(nil)
		return
	}
	service.observer.Store(&observer)
}

// Health 报告诊断子系统是否仍可创建新捕获；清理失败会保持降级直到重启。
func (service *Service) Health() error {
	if service.Statistics().CleanupFailures > 0 {
		return errors.New("runtime diagnostics cleanup failed")
	}
	return nil
}

// Statistics 汇报当前运行中的捕获数、目录占用与清理失败次数。
func (service *Service) Statistics() Statistics {
	service.mu.Lock()
	defer service.mu.Unlock()
	return Statistics{Active: service.active, Bytes: service.directoryBytesLocked(service.now()),
		CleanupFailures: service.cleanupFailures}
}
