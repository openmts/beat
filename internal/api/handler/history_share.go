package handler

import (
	"context"
	"sync"

	"github.com/beat/backend/internal/store"
)

// historyShareCapacity 是同一时刻允许合并的公共历史读取上限。表满时调用方直接读取，
// 因此这个上限只影响合并收益，不影响正确性。
const historyShareCapacity = 128

// metricHistoryShare 合并公共指标历史读取，networkHistoryShare 合并公共网络探测历史读取。
type (
	metricHistoryShare  = historyShare[map[string][]store.TimePoint]
	networkHistoryShare = historyShare[[]store.NetworkProbePoint]
)

// historyShare 让并发的相同公共历史请求共享一次存储读取：第一个调用方执行读取，其余
// 等待同一个结果，而不是每个观看者各打一次 MTS。结果集合在调用方之间只读共享。
type historyShare[T any] struct {
	mu    sync.Mutex
	calls map[string]*historyCall[T]
}

type historyCall[T any] struct {
	done   chan struct{}
	result T
	err    error
}

func newHistoryShare[T any]() *historyShare[T] {
	return &historyShare[T]{calls: make(map[string]*historyCall[T], historyShareCapacity)}
}

// do 复用 key 相同的在途读取；等待方尊重自己的上下文取消，nil 接收者退化为直读。
func (share *historyShare[T]) do(
	ctx context.Context,
	key string,
	read func(context.Context) (T, error),
) (T, error) {
	if share == nil {
		return read(ctx)
	}
	call, leader := share.join(key)
	if call == nil {
		// 表满：这次读取不参与合并，仍由调用方执行。
		return read(ctx)
	}
	if leader {
		call.result, call.err = read(ctx)
		share.finish(key, call)
		return call.result, call.err
	}
	select {
	case <-call.done:
		return call.result, call.err
	case <-ctx.Done():
		var empty T
		return empty, ctx.Err()
	}
}

// join 返回本次读取使用的调用记录；leader 为真表示由调用方执行读取，返回 nil 记录表示
// 合并表已满，调用方应直接读取。
func (share *historyShare[T]) join(key string) (*historyCall[T], bool) {
	share.mu.Lock()
	defer share.mu.Unlock()
	if call, ok := share.calls[key]; ok {
		return call, false
	}
	if len(share.calls) >= historyShareCapacity {
		return nil, false
	}
	call := &historyCall[T]{done: make(chan struct{})}
	share.calls[key] = call
	return call, true
}

func (share *historyShare[T]) finish(key string, call *historyCall[T]) {
	share.mu.Lock()
	if share.calls[key] == call {
		delete(share.calls, key)
	}
	share.mu.Unlock()
	close(call.done)
}

// historyShareKey 用 \x00 连接各字段拼出合并键：只有输入完全相同的读取才会命中同一条
// 记录，因此字段顺序是调用方的隐含契约（例如节点、区间、指标、桶宽、桶数）。
func historyShareKey(parts ...string) string {
	key := ""
	for index, part := range parts {
		if index > 0 {
			key += "\x00"
		}
		key += part
	}
	return key
}
