package metricquery

import (
	"context"
	"sync"
)

type admission struct {
	mu      sync.Mutex
	active  int
	clients map[string]int
}

func newAdmission() *admission { return &admission{clients: make(map[string]int)} }

func (budget *admission) acquire(ctx context.Context, client string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(client) > MaximumNodeIDBytes {
		return nil, ErrInvalid
	}
	if client == "" {
		client = "unknown"
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.active >= MaximumConcurrent || budget.clients[client] >= MaximumPerClient {
		return nil, ErrCapacity
	}
	budget.active++
	budget.clients[client]++
	return sync.OnceFunc(func() { budget.release(client) }), nil
}

func (budget *admission) release(client string) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.active--
	budget.clients[client]--
	if budget.clients[client] == 0 {
		delete(budget.clients, client)
	}
}
