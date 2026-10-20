package store

import (
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

func TestLegacyChannelMetadataReadsOnlyPublicRevisionAfterBackfill(t *testing.T) {
	database := setupTestDB(t)
	channels := NewAlertChannelStore(database.DB)
	created, err := channels.CreateAlertChannel(t.Context(), &model.AlertChannel{
		Name: "Migrated", ChannelType: "webhook", Config: `{"url":"https://receiver.example/PRIVATE_ENDPOINT","headers":{"Authorization":"PRIVATE_TOKEN"}}`, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = testutil.NewSecrets(t, database.DB)
	got, err := channels.GetAlertChannel(t.Context(), created.ID)
	if err != nil || got == nil || strings.Contains(got.Config, "PRIVATE_") || !strings.Contains(got.Config, "configured-host") {
		t.Fatalf("metadata read did not use the sanitized current revision: %v", err)
	}
	for _, read := range []func() ([]model.AlertChannel, error){
		func() ([]model.AlertChannel, error) { return channels.ListAlertChannels(t.Context()) },
		func() ([]model.AlertChannel, error) { return channels.ListEnabledChannels(t.Context()) },
	} {
		items, err := read()
		if err != nil || len(items) != 1 || strings.Contains(items[0].Config, "PRIVATE_") {
			t.Fatal("metadata list exposed encrypted revision inputs")
		}
	}
	if _, err := database.DB.Exec("UPDATE alert_channels SET deleted_at=CURRENT_TIMESTAMP"); err != nil {
		t.Fatal(err)
	}
	if got, err := channels.GetAlertChannel(t.Context(), created.ID); err != nil || got != nil {
		t.Fatal("soft deleted channel returned through metadata adapter")
	}
}
