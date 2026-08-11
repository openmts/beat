package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestAlertChannelStoreGet(t *testing.T) {
	store := setupTestDB(t)
	channels := NewAlertChannelStore(store.DB)
	created, err := channels.CreateAlertChannel(t.Context(), &model.AlertChannel{
		Name: "Webhook", ChannelType: "webhook", Config: `{"url":"https://example.com"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	got, err := channels.GetAlertChannel(t.Context(), created.ID)
	if err != nil || got == nil || got.Name != created.Name {
		t.Fatalf("channel = %#v, error = %v", got, err)
	}
	missing, err := channels.GetAlertChannel(t.Context(), "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing channel = %#v, error = %v", missing, err)
	}
}
