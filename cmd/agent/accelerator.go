package main

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/acceleratorcollect"
)

func acceleratorComponent(collector *acceleratorcollect.Collector) func(context.Context) error {
	return func(ctx context.Context) error {
		err := collector.Run(ctx)
		if err != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return err
	}
}
