package notificationqueue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestWorkerLimitIncreaseAppliesWithoutRestart(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	started := make(chan string, 12)
	s.sender = senderFunc(func(ctx context.Context, dispatch model.NotificationDispatch) model.NotificationSendResult {
		started <- dispatch.ChannelID
		<-ctx.Done()
		return model.NotificationSendResult{Retryable: true, ErrorCode: "request_cancelled"}
	})
	for index := range 12 {
		id := fmt.Sprintf("channel-%d", index)
		addQueueChannel(t, s, id)
		request := enqueueRequest(id)
		request.UseSubscriptions, request.ChannelIDs = false, []string{id}
		if _, err := s.Enqueue(t.Context(), request, nil); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	awaitStartedDeliveries(t, started, 8)
	select {
	case <-started:
		t.Fatal("default worker concurrency exceeded eight")
	case <-time.After(30 * time.Millisecond):
	}
	write := model.NotificationSettingsWrite{ExpectedRevision: 1, SuccessRetentionDays: 30, DeadRetentionDays: 90, MaximumWorkers: 12}
	if _, err := s.UpdateSettings(t.Context(), actor, write); err != nil {
		t.Fatal(err)
	}
	awaitStartedDeliveries(t, started, 4)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func awaitStartedDeliveries(t *testing.T, started <-chan string, count int) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for range count {
		select {
		case <-started:
		case <-timer.C:
			t.Fatal("configured notification worker limit did not take effect")
		}
	}
}
