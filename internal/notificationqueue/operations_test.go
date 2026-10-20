package notificationqueue

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestDurableTestBudgetDisableDeleteAndHistory(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	addQueueChannel(t, s, "channel")
	if _, err := s.SetEnabled(t.Context(), actor, ChannelEnabled{ID: "channel", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	var first Receipt
	for index := range 5 {
		actor.RequestID = fmt.Sprintf("test-%d", index)
		receipt, err := s.TestChannel(t.Context(), actor, "channel")
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = receipt
		}
	}
	actor.RequestID = "over-budget"
	if _, err := s.TestChannel(t.Context(), actor, "channel"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("test budget not enforced")
	}
	actor.RequestID = "test-0"
	repeated, err := s.TestChannel(t.Context(), actor, "channel")
	if err != nil || !repeated.Duplicate || repeated.MessageID != first.MessageID {
		t.Fatal("test retry duplicated message")
	}
	lease, err := s.claim(t.Context(), "worker")
	if err != nil || lease == nil {
		t.Fatal("disabled channel test did not reach same worker")
	}
	if err := s.complete(t.Context(), completion{lease: *lease, result: model.NotificationSendResult{Success: true}}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListDeliveries(t.Context(), DeliveryFilter{ChannelID: "channel", Limit: 10})
	if err != nil || page.Total != 5 {
		t.Fatal(err)
	}
	detail, err := s.GetDelivery(t.Context(), lease.ID)
	if err != nil || len(detail.Attempts) != 1 || detail.Attempts[0].Outcome != "succeeded" {
		t.Fatal("attempt history missing")
	}
	if err := s.DeleteChannel(t.Context(), actor, "channel"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChannel(t.Context(), "channel"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted channel listed")
	}
	page, err = s.ListDeliveries(t.Context(), DeliveryFilter{State: "cancelled"})
	if err != nil || page.Total != 4 {
		t.Fatal("deletion lost queued evidence")
	}
}

func TestManualRetryUsesCurrentRevisionAndCancellationPreservesHistory(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	addQueueChannel(t, s, "channel")
	receipt, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.claim(t.Context(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.complete(t.Context(), completion{lease: *lease, result: model.NotificationSendResult{ErrorCode: "http_rejected", Stage: "response", StatusClass: "4xx"}}); err != nil {
		t.Fatal(err)
	}
	write := model.NotificationChannelWrite{Name: "Updated", ChannelType: "webhook", Enabled: true, ExpectedRevision: 1, EventKinds: []string{"alert.triggered"}, Config: json.RawMessage(`{"endpoint":"https://8.8.8.8/fixed"}`)}
	if _, err := s.UpdateChannel(t.Context(), actor, ChannelUpdate{ID: "channel", Write: write}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: lease.ID}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("duplicate confirmation bypassed")
	}
	retry, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: lease.ID, ConfirmDuplicate: true})
	if err != nil || retry.ID == lease.ID || retry.ChannelRevision != 2 {
		t.Fatal("manual retry not pinned to corrected revision")
	}
	duplicate, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: lease.ID, ConfirmDuplicate: true})
	if err != nil || duplicate.ID != retry.ID {
		t.Fatal("manual retry duplicated")
	}
	if err := s.CancelDelivery(t.Context(), actor, retry.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.SourceReceipt(t.Context(), "event")
	if err != nil || current.State != "cancelled" || current.MessageID != receipt.MessageID {
		t.Fatal("cancelled retry leaf missing")
	}
	if _, err := s.RetryDelivery(t.Context(), actor, RetryRequest{ID: retry.ID, ConfirmDuplicate: true}); !errors.Is(err, ErrConflict) {
		t.Fatal("cancelled record retried")
	}
	original, err := s.GetDelivery(t.Context(), lease.ID)
	if err != nil || original.State != "dead" || len(original.Attempts) != 1 {
		t.Fatal("original failure evidence erased")
	}
}

func TestSettingsAndPreviewAreAuditedAndRedacted(t *testing.T) {
	s, _ := queueFixture(t)
	actor := queueOwner(t, s)
	settings, err := s.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	write := model.NotificationSettingsWrite{ExpectedRevision: settings.Revision, SuccessRetentionDays: 40, DeadRetentionDays: 100, MaximumWorkers: 4,
		EgressPolicy: model.NotificationEgressPolicy{Exceptions: []model.NotificationEgressException{{Host: "127.0.0.1", Port: 8080, Purpose: "local stub", ExpiresAt: s.now().Add(time.Hour)}}}}
	updated, err := s.UpdateSettings(t.Context(), actor, write)
	if err != nil || updated.Revision != 2 || updated.EgressPolicy.Exceptions[0].CreatedBy != actor.UserID {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettings(t.Context(), actor, write); !errors.Is(err, ErrConflict) {
		t.Fatal("stale settings update accepted")
	}
	preview, err := s.Preview(t.Context(), actor, PreviewWrite{ChannelType: "custom_http", Config: json.RawMessage(`{"endpoint":"https://PRIVATE_HOST.example/PRIVATE_PATH?key=PRIVATE_QUERY","headers":{"Authorization":"PRIVATE_HEADER"},"body_mode":"json","body":{"secret":"{{secret.token}}","text":"{{text}}"},"named_secrets":{"token":"PRIVATE_TOKEN"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE_HOST", "PRIVATE_PATH", "PRIVATE_QUERY", "PRIVATE_HEADER", "PRIVATE_TOKEN"} {
		if containsBytes(encoded, secret) {
			t.Fatal("preview exposed stored secret")
		}
	}
}

func containsBytes(value []byte, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if string(value[index:index+len(needle)]) == needle {
			return true
		}
	}
	return false
}
