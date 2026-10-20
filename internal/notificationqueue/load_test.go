package notificationqueue

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

func TestTenThousandDeliveriesSurviveDispatchRestart(t *testing.T) {
	s, db := queueFixture(t)
	s.workers = 4
	clock := time.Now().UTC()
	s.now = func() time.Time { return clock }
	for index := range 8 {
		addQueueChannel(t, s, fmt.Sprintf("channel-%d", index))
	}
	const deliveries = 10000
	err := s.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		tx, err := db.DB.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		for index := range deliveries {
			request := enqueueRequest(fmt.Sprintf("event-%d", index))
			request.UseSubscriptions = false
			request.ChannelIDs = []string{fmt.Sprintf("channel-%d", index%8)}
			if _, err := s.enqueueTx(t.Context(), tx, enqueueInput{request: request, access: access}); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatal(err)
	}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	var active, maximum atomic.Int32
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	var mu sync.Mutex
	byChannel := map[string]bool{}
	s.sender = senderFunc(func(ctx context.Context, dispatch model.NotificationDispatch) model.NotificationSendResult {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		value := active.Add(1)
		for previous := maximum.Load(); value > previous && !maximum.CompareAndSwap(previous, value); previous = maximum.Load() {
		}
		mu.Lock()
		if byChannel[dispatch.ChannelID] {
			t.Error("parallel delivery on one channel")
		}
		byChannel[dispatch.ChannelID] = true
		mu.Unlock()
		defer func() { active.Add(-1); mu.Lock(); delete(byChannel, dispatch.ChannelID); mu.Unlock() }()
		select {
		case <-ctx.Done():
			return model.NotificationSendResult{Retryable: true, ErrorCode: "request_cancelled", Stage: "request"}
		case <-time.After(time.Millisecond):
			return model.NotificationSendResult{Success: true}
		}
	})
	for range 2 {
		before := calls.Load()
		runLoadSlice(t, s, started)
		if calls.Load() <= before {
			t.Fatal("restarted worker did not resume dispatch")
		}
		clock = clock.Add(time.Minute)
	}
	var count, unique int
	if err := db.DB.QueryRow("SELECT COUNT(*),COUNT(DISTINCT id) FROM notification_deliveries").Scan(&count, &unique); err != nil || count != deliveries || unique != deliveries {
		t.Fatal("dispatch restart lost or duplicated queue rows")
	}
	if maximum.Load() > 4 || maximum.Load() < 1 {
		t.Fatal("worker concurrency limit failed")
	}
	var leased int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM notification_deliveries WHERE state='leased'").Scan(&leased); err != nil || leased != 0 {
		t.Fatalf("shutdown left in-flight leases: %d, %v", leased, err)
	}
}

func runLoadSlice(t *testing.T, s *Service, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	default:
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("worker stopped before dispatch: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		stopErr := <-done
		t.Fatalf("worker did not start dispatch; shutdown result: %v", stopErr)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
