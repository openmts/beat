package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func seedMessageEvent(
	t *testing.T, store *AdminStore, eventID, messageID string, at time.Time,
) {
	t.Helper()
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin message event: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	event := model.SecurityEvent{ID: eventID, Kind: model.SecurityEventLoginSucceeded,
		Severity: model.SecuritySeverityInfo, AuthMethod: model.SecurityAuthMethodPassword,
		SourceClass: model.SecuritySourceClassPublic, SourceFamily: model.SecuritySourceFamilyIPv4,
		SourcePrefix: "203.0.113.0/24", BrowserFamily: "chrome", OSFamily: "linux",
		DeviceClass: model.SecurityDeviceClassDesktop, Reasons: []string{model.SecurityReasonNewSourcePrefix},
		PolicyRevision: 1, SourceKey: "key:" + eventID, SourceKind: SecurityEventSourceKind,
		RoutingState: model.SecurityRoutingRouted, MessageID: messageID, OccurredAt: at,
		RetainUntil: at.AddDate(0, 0, 180)}
	if err := store.InsertSecurityEventsTx(t.Context(), tx, []model.SecurityEvent{event}); err != nil {
		t.Fatalf("seed message event: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit message event: %v", err)
	}
}

// seedNotificationMessage 先写入通知消息；安全事件的 message_id 通过外键指向它。
func seedNotificationMessage(t *testing.T, store *AdminStore, messageID string, at time.Time) {
	t.Helper()
	if _, err := store.db.Exec(`INSERT INTO notification_messages (id,source_key,source_kind,source_id,event_kind,
		payload_encrypted,payload_version,routing_state,selected_channels,created_at,retain_until)
		VALUES(?,?,?,?,'security.login_succeeded',?,1,'routed',1,?,?)`,
		messageID, "key:"+messageID, SecurityEventSourceKind, messageID, make([]byte, 64), at.UnixMilli(),
		at.AddDate(0, 0, 30).UnixMilli()); err != nil {
		t.Fatalf("seed message: %v", err)
	}
}

// seedSecurityDelivery 写入通道与投递行，复现通知队列留下的脱敏投递证据。
func seedSecurityDelivery(
	t *testing.T, store *AdminStore, messageID, channelName, state string, attempts int, errorCode string, at time.Time,
) {
	t.Helper()
	moment := at.UnixMilli()
	channelID, revisionID, deliveryID := "channel-"+channelName, "revision-"+channelName, "delivery-"+channelName
	if _, err := store.db.Exec(`INSERT INTO alert_channels (id,name,channel_type,config,enabled,created_at,updated_at)
		VALUES(?,?,'webhook','{}',1,?,?)`, channelID, channelName, at, at); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO alert_channel_revisions (id,channel_id,revision,channel_type,
		public_config_json,secret_ciphertext,secret_present_json,created_by,created_at,change_reason)
		VALUES(?,?,1,'webhook','{}',?,'{"secret":true}','owner',?,'seed')`,
		revisionID, channelID, make([]byte, 64), moment); err != nil {
		t.Fatalf("seed channel revision: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO notification_deliveries (id,message_id,channel_id,channel_revision,
		state,attempt_count,next_attempt_at,worker_id,error_code,error_stage,status_class,created_at,updated_at)
		VALUES(?,?,?,1,?,?,?,'',?,'','',?,?)`,
		deliveryID, messageID, channelID, state, attempts, moment, errorCode, moment, moment); err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
}

// seedMessageEvent 写入带消息链接的事件，用于投递证据展示。
func TestGetSecurityEventCarriesSanitizedDeliveryEvidence(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	seedNotificationMessage(t, store, "msg-delivery", now)
	seedMessageEvent(t, store, "event-delivery", "msg-delivery", now)
	seedSecurityDelivery(t, store, "msg-delivery", "Operations", "dead", 2, "provider_rejected", now)

	event, err := store.GetSecurityEvent(t.Context(), "event-delivery")
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	if event.DeliveryState != securityDeliveryFailed || event.DeliveryFailures != 1 ||
		event.DeliveryChannels != 1 || event.DeliveryPending != 0 {
		t.Fatalf("delivery counters = %+v", event)
	}
	if len(event.Deliveries) != 1 {
		t.Fatalf("deliveries = %+v", event.Deliveries)
	}
	delivery := event.Deliveries[0]
	if delivery.ChannelName != "Operations" || delivery.State != "dead" || delivery.AttemptCount != 2 ||
		delivery.ErrorCode != "provider_rejected" || delivery.UpdatedAt.IsZero() {
		t.Fatalf("delivery = %+v", delivery)
	}
}

func TestListSecurityEventsReportsEveryDeliveryState(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	states := map[string]string{
		"event-failed":    "dead",
		"event-pending":   "pending",
		"event-delivered": "succeeded",
	}
	offset := 0
	for eventID, state := range states {
		seedNotificationMessage(t, store, "msg-"+eventID, now.Add(time.Duration(offset)*time.Minute))
		seedMessageEvent(t, store, eventID, "msg-"+eventID, now.Add(time.Duration(offset)*time.Minute))
		seedSecurityDelivery(t, store, "msg-"+eventID, "Channel-"+eventID, state, 1, "", now)
		offset++
	}
	seedSecurityEvent(t, store, "event-none", model.SecurityEventLoginSucceeded, model.SecuritySeverityInfo,
		model.SecurityReasonNewSourcePrefix, model.SecuritySourceClassPublic, "203.0.113.0/24", now.Add(-time.Minute))

	page, err := store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{})
	if err != nil || page.Total != 4 {
		t.Fatalf("page = %+v / %v", page, err)
	}
	got := map[string]string{}
	for _, event := range page.Events {
		got[event.ID] = event.DeliveryState
	}
	want := map[string]string{"event-failed": securityDeliveryFailed, "event-pending": securityDeliveryPending,
		"event-delivered": securityDeliveryDelivered, "event-none": securityDeliveryNone}
	for id, state := range want {
		if got[id] != state {
			t.Fatalf("event %s delivery state = %q, want %q", id, got[id], state)
		}
	}
}

func TestListSecurityEventsNormalizesBounds(t *testing.T) {
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	page, err := store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Limit: 0, Offset: -5})
	if err != nil || page.Limit != securityEventDefaultLimit || page.Offset != 0 {
		t.Fatalf("default bounds = %+v / %v", page, err)
	}
	page, err = store.ListSecurityEvents(t.Context(), model.SecurityEventFilter{Limit: 500})
	if err != nil || page.Limit != securityEventMaximumLimit {
		t.Fatalf("capped bounds = %+v / %v", page, err)
	}
}
