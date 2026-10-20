package notificationqueue

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestWorkerStorageFailureStopsPoolAndDegradesReadiness(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	service := fixture.service
	var observations atomic.Int64
	service.SetObserver(func(err error) {
		if err != nil {
			observations.Add(1)
		}
	})
	service.sender = senderFunc(func(context.Context, model.NotificationDispatch) model.NotificationSendResult {
		return model.NotificationSendResult{Success: true}
	})
	service.secrets = failingResourceService{base: service.secrets, write: true}
	if err := service.Run(t.Context()); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatal("worker persistence failure was hidden")
	}
	if observations.Load() == 0 || !errors.Is(service.Ready(t.Context()), ErrQueueUnavailable) {
		t.Fatal("queue failure was not observable")
	}
	service.Close()
}

func TestCloseCancelsCurrentRequestAndReleasesLeaseBeforeReturning(t *testing.T) {
	fixture := newQueueFaultFixture(t)
	service := fixture.service
	started := make(chan struct{})
	service.sender = senderFunc(func(ctx context.Context, _ model.NotificationDispatch) model.NotificationSendResult {
		close(started)
		<-ctx.Done()
		return model.NotificationSendResult{Retryable: true, ErrorCode: "request_cancelled"}
	})
	done := make(chan error, 1)
	go func() { done <- service.Run(t.Context()) }()
	<-started
	if err := service.Run(t.Context()); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatal("same queue was started twice")
	}
	service.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='pending' AND lease_token_hash IS NULL", 1)
	fixture.assertCount(t, "SELECT COUNT(*) FROM notification_delivery_attempts WHERE outcome='retryable'", 1)
}

func TestCancelDeliveryDoesNotCancelAnotherRequestOnChannel(t *testing.T) {
	service, _ := queueFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service.active["channel"] = activeDelivery{id: "active", cancel: cancel}
	service.cancelDelivery("channel", "unrelated")
	if ctx.Err() != nil {
		t.Fatal("pending delivery cancellation interrupted a different active request")
	}
	service.cancelDelivery("channel", "active")
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("matching active delivery was not cancelled")
	}
}

func TestProcessRechecksPauseAndCancellationBeforeSending(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{true: "cancelled", false: "disabled"}[cancelled], func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			lease := fixture.lease(t)
			fixture.service.sender = senderFunc(func(context.Context, model.NotificationDispatch) model.NotificationSendResult {
				t.Error("revoked work reached sender")
				return model.NotificationSendResult{}
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			} else {
				fixture.sql(t, "UPDATE alert_channels SET enabled=0")
			}
			if err := fixture.service.process(ctx, lease); err != nil {
				t.Fatal(err)
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='pending'", 1)
		})
	}
}

func TestProcessFailsClosedOnSecretReadAndCompletionFailure(t *testing.T) {
	for _, reading := range []bool{false, true} {
		t.Run(map[bool]string{true: "reading", false: "completion"}[reading], func(t *testing.T) {
			fixture := newQueueFaultFixture(t)
			lease := fixture.lease(t)
			fixture.service.sender = senderFunc(func(context.Context, model.NotificationDispatch) model.NotificationSendResult {
				return model.NotificationSendResult{Success: true}
			})
			fixture.service.secrets = failingResourceService{base: fixture.service.secrets, open: reading, write: !reading}
			if err := fixture.service.process(t.Context(), lease); !errors.Is(err, ErrQueueUnavailable) {
				t.Fatal("processing ignored unavailable secret storage")
			}
			fixture.assertCount(t, "SELECT COUNT(*) FROM notification_deliveries WHERE state='leased'", 1)
		})
	}
}

// 关闭期间上下文取消会让 SQLite 自动回滚，驱动随后把后续语句报成 sql.ErrTxDone。
// 监督器不能把这种存储错误当成故障：work 在同一次取消下返回 nil，两边必须一致。
func TestWorkerNormalizesCancelledStorageFailureForObserver(t *testing.T) {
	service, _ := queueFixture(t)
	var observed []error
	service.SetObserver(func(err error) { observed = append(observed, err) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service.reportFailure(ctx, sql.ErrTxDone)
	if len(observed) != 1 || !errors.Is(observed[0], context.Canceled) {
		t.Fatalf("shutdown observation = %v", observed)
	}

	observed = nil
	service.reportFailure(t.Context(), sql.ErrTxDone)
	if len(observed) != 1 || !errors.Is(observed[0], sql.ErrTxDone) {
		t.Fatalf("live observation = %v", observed)
	}
}
