package handler

import (
	"context"
	"sync"
)

const (
	maximumConcurrentAgentReports = 32
	maximumClientAgentReports     = 8
)

type agentIngestBudget struct {
	mu      sync.Mutex
	active  int
	clients map[string]int
	nodes   map[string]bool
}

func (budget *agentIngestBudget) acquire(ctx context.Context, client, node string) (func(), bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.active >= maximumConcurrentAgentReports || budget.clients[client] >= maximumClientAgentReports || budget.nodes[node] {
		return nil, false
	}
	if budget.clients == nil {
		budget.clients = make(map[string]int)
		budget.nodes = make(map[string]bool)
	}
	budget.active++
	budget.clients[client]++
	budget.nodes[node] = true
	var once sync.Once
	return func() { once.Do(func() { budget.release(client, node) }) }, true
}

func (budget *agentIngestBudget) release(client, node string) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.active--
	budget.clients[client]--
	if budget.clients[client] == 0 {
		delete(budget.clients, client)
	}
	delete(budget.nodes, node)
}
