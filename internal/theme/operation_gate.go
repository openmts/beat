package theme

import (
	"context"
	"sync"
)

type operationGate struct {
	once    sync.Once
	pending chan struct{}
}

func (gate *operationGate) enter(ctx context.Context) (func(), error) {
	gate.once.Do(func() { gate.pending = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case gate.pending <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate.pending
			return nil, err
		}
		return func() { <-gate.pending }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
