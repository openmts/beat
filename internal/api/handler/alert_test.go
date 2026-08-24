package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestHandleListAlertRules(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	_, _ = alertRuleStore.CreateAlertRule(ctx, &model.AlertRule{
		Name:      "Rule 1",
		Metric:    "cpu",
		Operator:  ">",
		Threshold: 90,
		Duration:  300,
		Severity:  model.AlertSeverityWarning,
		Enabled:   true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/alert-rules", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListAlertRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleCreateAlertRule(t *testing.T) {
	t.Run("creates alert rule", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		alertRuleStore := store.NewAlertRuleStore(s.DB)
		alertChannelStore := store.NewAlertChannelStore(s.DB)
		alertEventStore := store.NewAlertEventStore(s.DB)
		h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

		body := `{"name": "High CPU", "metric": "cpu", "operator": ">", "threshold": 90, "duration": 300, "severity": "warning"}`
		req := httptest.NewRequest(http.MethodPost, "/api/alert-rules", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateAlertRule(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		alertRuleStore := store.NewAlertRuleStore(s.DB)
		alertChannelStore := store.NewAlertChannelStore(s.DB)
		alertEventStore := store.NewAlertEventStore(s.DB)
		h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

		body := `{"name": "Incomplete Rule"}`
		req := httptest.NewRequest(http.MethodPost, "/api/alert-rules", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateAlertRule(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		alertRuleStore := store.NewAlertRuleStore(s.DB)
		alertChannelStore := store.NewAlertChannelStore(s.DB)
		alertEventStore := store.NewAlertEventStore(s.DB)
		h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/api/alert-rules", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateAlertRule(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleUpdateAlertRule(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	rule, err := alertRuleStore.CreateAlertRule(ctx, &model.AlertRule{
		Name:      "Old Rule",
		Metric:    "cpu",
		Operator:  ">",
		Threshold: 80,
		Duration:  60,
		Severity:  model.AlertSeverityInfo,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := `{"name": "Updated Rule", "metric": "cpu", "operator": ">", "threshold": 95, "duration": 600, "severity": "critical"}`
	req := httptest.NewRequest(http.MethodPut, "/api/alert-rules/"+rule.ID, strings.NewReader(body))
	req = req.WithContext(ctx)
	req.SetPathValue("id", rule.ID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleUpdateAlertRule(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleDeleteAlertRule(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	rule, err := alertRuleStore.CreateAlertRule(ctx, &model.AlertRule{
		Name:      "To Delete",
		Metric:    "cpu",
		Operator:  ">",
		Threshold: 50,
		Duration:  0,
		Severity:  model.AlertSeverityInfo,
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/alert-rules/"+rule.ID, nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", rule.ID)
	w := httptest.NewRecorder()

	h.HandleDeleteAlertRule(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestHandleListAlertChannels(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	_, _ = alertChannelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name:        "Channel 1",
		ChannelType: "email",
		Config:      `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["admin@example.com"],"security":"starttls"}`,
		Enabled:     true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/alert-channels", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListAlertChannels(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleCreateAlertChannel(t *testing.T) {
	t.Run("creates alert channel", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		alertRuleStore := store.NewAlertRuleStore(s.DB)
		alertChannelStore := store.NewAlertChannelStore(s.DB)
		alertEventStore := store.NewAlertEventStore(s.DB)
		h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

		body := `{"name":"Email Channel","channel_type":"email","config":"{\"host\":\"smtp.example.com\",\"port\":587,\"from\":\"beat@example.com\",\"to\":[\"admin@example.com\"],\"security\":\"starttls\"}","enabled":true}`
		req := httptest.NewRequest(http.MethodPost, "/api/alert-channels", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateAlertChannel(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		alertRuleStore := store.NewAlertRuleStore(s.DB)
		alertChannelStore := store.NewAlertChannelStore(s.DB)
		alertEventStore := store.NewAlertEventStore(s.DB)
		h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

		body := `{"name": "Incomplete Channel"}`
		req := httptest.NewRequest(http.MethodPost, "/api/alert-channels", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateAlertChannel(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		alertRuleStore := store.NewAlertRuleStore(s.DB)
		alertChannelStore := store.NewAlertChannelStore(s.DB)
		alertEventStore := store.NewAlertEventStore(s.DB)
		h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/api/alert-channels", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateAlertChannel(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleUpdateAlertChannel(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	channel, err := alertChannelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name:        "Old Channel",
		ChannelType: "email",
		Config:      `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["old@example.com"],"security":"starttls"}`,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := `{"name":"Updated Channel","channel_type":"webhook","config":"{\"url\":\"https://hooks.example.com/beat\"}","enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/api/alert-channels/"+channel.ID, strings.NewReader(body))
	req = req.WithContext(ctx)
	req.SetPathValue("id", channel.ID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleUpdateAlertChannel(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleDeleteAlertChannel(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	channel, err := alertChannelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name:        "To Delete",
		ChannelType: "email",
		Config:      `{"host":"smtp.example.com","port":587,"from":"beat@example.com","to":["admin@example.com"],"security":"starttls"}`,
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/alert-channels/"+channel.ID, nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", channel.ID)
	w := httptest.NewRecorder()

	h.HandleDeleteAlertChannel(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestHandleListAlertEvents(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	alertRuleStore := store.NewAlertRuleStore(s.DB)
	alertChannelStore := store.NewAlertChannelStore(s.DB)
	alertEventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(alertRuleStore, alertChannelStore, alertEventStore, nil)

	_ = alertEventStore.CreateEvent(ctx, &model.AlertEvent{
		ID:          "event-1",
		RuleID:      "rule-1",
		NodeID:      "node-1",
		Message:     "CPU above threshold",
		Value:       95.5,
		Status:      model.AlertStatusTriggered,
		TriggeredAt: model.NowUTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/alert-events", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListAlertEvents(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleAcknowledgeAlertEvent(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	eventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), store.NewAlertChannelStore(s.DB), eventStore, store.NewNodeStore(s.DB))

	event := &model.AlertEvent{
		ID: "ev-ack", RuleID: "rule-1", NodeID: "node-1",
		Message: "CPU high", Value: 90, Status: model.AlertStatusTriggered,
		TriggeredAt: model.NowUTC(),
	}
	_ = eventStore.CreateEvent(ctx, event)

	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-ack/acknowledge", nil)
	req.SetPathValue("id", "ev-ack")
	w := httptest.NewRecorder()
	h.HandleAcknowledgeAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got, _ := eventStore.GetEventByID(ctx, "ev-ack")
	if got.Status != model.AlertStatusAcknowledged || got.AcknowledgedAt == nil {
		t.Fatalf("after ack = %s, ack_at = %#v", got.Status, got.AcknowledgedAt)
	}
}

func TestHandleAcknowledgeAlertEventWrongState(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	eventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), store.NewAlertChannelStore(s.DB), eventStore, store.NewNodeStore(s.DB))

	now := model.NowUTC()
	// Already acknowledged.
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-1", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusAcknowledged,
		TriggeredAt: now, AcknowledgedAt: &now,
	})
	// Already resolved.
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-2", RuleID: "rule-2", NodeID: "node-1", Status: model.AlertStatusResolved,
		TriggeredAt: now, ResolvedAt: &now,
	})
	for _, id := range []string{"ev-1", "ev-2", "missing"} {
		req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/"+id+"/acknowledge", nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.HandleAcknowledgeAlertEvent(w, req)
		if id == "missing" {
			if w.Code != http.StatusNotFound {
				t.Fatalf("missing ack status = %d, want 404", w.Code)
			}
			continue
		}
		if w.Code != http.StatusConflict {
			t.Fatalf("ack %s status = %d, want 409", id, w.Code)
		}
	}
}

func TestHandleResolveAlertEvent(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	eventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), store.NewAlertChannelStore(s.DB), eventStore, store.NewNodeStore(s.DB))

	now := model.NowUTC()
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-r", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusAcknowledged,
		TriggeredAt: now, AcknowledgedAt: &now,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-r/resolve", nil)
	req.SetPathValue("id", "ev-r")
	w := httptest.NewRecorder()
	h.HandleResolveAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got, _ := eventStore.GetEventByID(ctx, "ev-r")
	if got.Status != model.AlertStatusResolved || got.ResolvedAt == nil {
		t.Fatalf("after resolve = %s, resolved_at = %#v", got.Status, got.ResolvedAt)
	}
}

func TestHandleResolveAlertEventWrongState(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	eventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), store.NewAlertChannelStore(s.DB), eventStore, store.NewNodeStore(s.DB))

	now := model.NowUTC()
	// Triggered events cannot be directly resolved (must acknowledge first).
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-t", RuleID: "rule-t", NodeID: "node-1", Status: model.AlertStatusTriggered,
		TriggeredAt: now,
	})
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-res", RuleID: "rule-x", NodeID: "node-1", Status: model.AlertStatusResolved,
		TriggeredAt: now, ResolvedAt: &now,
	})
	for _, id := range []string{"ev-t", "ev-res"} {
		req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/"+id+"/resolve", nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.HandleResolveAlertEvent(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("resolve %s status = %d, want 409", id, w.Code)
		}
	}
}

func TestHandleAcknowledgePushesNotification(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	eventStore := store.NewAlertEventStore(s.DB)
	// Enabled webhook channel so notifyAcknowledgment sends a delivery.
	channelStore := store.NewAlertChannelStore(s.DB)
	_, _ = channelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name: "Hook", ChannelType: "webhook", Config: `{"url":"https://example.com/hook"}`, Enabled: true,
	})
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), channelStore, eventStore, store.NewNodeStore(s.DB))

	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-n", RuleID: "rule-1", NodeID: "node-1",
		Message: "CPU high", Value: 90, Status: model.AlertStatusTriggered,
		TriggeredAt: model.NowUTC(),
	})
	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-n/acknowledge", nil)
	req.SetPathValue("id", "ev-n")
	w := httptest.NewRecorder()
	h.HandleAcknowledgeAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got, _ := eventStore.GetEventByID(ctx, "ev-n")
	if got.Status != model.AlertStatusAcknowledged {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestHandleResolvePushesNotification(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	eventStore := store.NewAlertEventStore(s.DB)
	channelStore := store.NewAlertChannelStore(s.DB)
	_, _ = channelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name: "Hook", ChannelType: "webhook", Config: `{"url":"https://example.com/hook"}`, Enabled: true,
	})
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), channelStore, eventStore, store.NewNodeStore(s.DB))

	now := model.NowUTC()
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-rn", RuleID: "rule-1", NodeID: "node-1", Status: model.AlertStatusAcknowledged,
		TriggeredAt: now, AcknowledgedAt: &now,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-rn/resolve", nil)
	req.SetPathValue("id", "ev-rn")
	w := httptest.NewRecorder()
	h.HandleResolveAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got, _ := eventStore.GetEventByID(ctx, "ev-rn")
	if got.Status != model.AlertStatusResolved || got.ResolvedAt == nil {
		t.Fatalf("after resolve = %s", got.Status)
	}
}

func TestHandleAcknowledgeEventStoreError(t *testing.T) {
	s := setupTestDB(t)
	_ = s.Close()
	eventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), store.NewAlertChannelStore(s.DB), eventStore, store.NewNodeStore(s.DB))

	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/x/acknowledge", nil)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	h.HandleAcknowledgeAlertEvent(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleResolveEventStoreError(t *testing.T) {
	s := setupTestDB(t)
	_ = s.Close()
	eventStore := store.NewAlertEventStore(s.DB)
	h := NewAlertHandler(store.NewAlertRuleStore(s.DB), store.NewAlertChannelStore(s.DB), eventStore, store.NewNodeStore(s.DB))

	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/x/resolve", nil)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	h.HandleResolveAlertEvent(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleAcknowledgeEnrichesNotificationContext(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	ruleStore := store.NewAlertRuleStore(s.DB)
	rule, err := ruleStore.CreateAlertRule(ctx, &model.AlertRule{
		Name: "High Memory", Metric: "memory", Operator: "gt",
		Threshold: 15, Severity: model.AlertSeverityWarning, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	nodeStore := store.NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "edge-node", "10.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	var delivered model.AlertEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	channelStore := store.NewAlertChannelStore(s.DB)
	_, _ = channelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name: "Hook", ChannelType: "webhook", Config: server.URL, Enabled: true,
	})

	eventStore := store.NewAlertEventStore(s.DB)
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-ack-ctx", RuleID: rule.ID, NodeID: node.ID,
		Message: "memory high", Value: 53, Status: model.AlertStatusTriggered,
		TriggeredAt: model.NowUTC(),
	})

	h := NewAlertHandler(ruleStore, channelStore, eventStore, nodeStore)
	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-ack-ctx/acknowledge", nil)
	req.SetPathValue("id", "ev-ack-ctx")
	w := httptest.NewRecorder()
	h.HandleAcknowledgeAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	if delivered.RuleName != "High Memory" {
		t.Errorf("RuleName = %q, want High Memory", delivered.RuleName)
	}
	if delivered.NodeName != "edge-node" {
		t.Errorf("NodeName = %q, want edge-node", delivered.NodeName)
	}
	if delivered.Metric != "memory" {
		t.Errorf("Metric = %q, want memory", delivered.Metric)
	}
	if delivered.Severity != model.AlertSeverityWarning {
		t.Errorf("Severity = %q, want warning", delivered.Severity)
	}
	if delivered.Status != model.AlertStatusAcknowledged {
		t.Errorf("Status = %q, want acknowledged", delivered.Status)
	}
	if !strings.Contains(delivered.Message, "acknowledged") ||
		!strings.Contains(delivered.Message, "High Memory") ||
		!strings.Contains(delivered.Message, "edge-node") {
		t.Errorf("Message = %q, want enriched ack message", delivered.Message)
	}
}

func TestHandleResolveEnrichesNotificationContext(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	ruleStore := store.NewAlertRuleStore(s.DB)
	rule, err := ruleStore.CreateAlertRule(ctx, &model.AlertRule{
		Name: "CPU Spike", Metric: "cpu", Operator: "gt",
		Threshold: 80, Severity: model.AlertSeverityCritical, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	nodeStore := store.NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "web-01", "10.0.0.2", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	var delivered model.AlertEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&delivered); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	channelStore := store.NewAlertChannelStore(s.DB)
	_, _ = channelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name: "Hook", ChannelType: "webhook", Config: server.URL, Enabled: true,
	})

	now := model.NowUTC()
	eventStore := store.NewAlertEventStore(s.DB)
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-res-ctx", RuleID: rule.ID, NodeID: node.ID,
		Value: 95, Status: model.AlertStatusAcknowledged,
		TriggeredAt: now, AcknowledgedAt: &now,
	})

	h := NewAlertHandler(ruleStore, channelStore, eventStore, nodeStore)
	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-res-ctx/resolve", nil)
	req.SetPathValue("id", "ev-res-ctx")
	w := httptest.NewRecorder()
	h.HandleResolveAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	if delivered.RuleName != "CPU Spike" {
		t.Errorf("RuleName = %q, want CPU Spike", delivered.RuleName)
	}
	if delivered.NodeName != "web-01" {
		t.Errorf("NodeName = %q, want web-01", delivered.NodeName)
	}
	if delivered.Metric != "cpu" {
		t.Errorf("Metric = %q, want cpu", delivered.Metric)
	}
	if delivered.Severity != model.AlertSeverityCritical {
		t.Errorf("Severity = %q, want critical", delivered.Severity)
	}
	if delivered.Status != model.AlertStatusResolved || delivered.ResolvedAt == nil {
		t.Errorf("Status = %q, ResolvedAt = %#v, want resolved", delivered.Status, delivered.ResolvedAt)
	}
	if !strings.Contains(delivered.Message, "resolved") ||
		!strings.Contains(delivered.Message, "CPU Spike") ||
		!strings.Contains(delivered.Message, "web-01") {
		t.Errorf("Message = %q, want enriched resolve message", delivered.Message)
	}
}

func TestHandleAcknowledgeRendersTemplatePlaceholders(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	ruleStore := store.NewAlertRuleStore(s.DB)
	rule, err := ruleStore.CreateAlertRule(ctx, &model.AlertRule{
		Name: "High Memory", Metric: "memory", Operator: "gt",
		Threshold: 15, Severity: model.AlertSeverityWarning, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	nodeStore := store.NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "edge-node", "10.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	tmpl := `[{{severity}}] {{rule}} on {{node}}: {{metric}}={{value}} ({{status}})`
	channelStore := store.NewAlertChannelStore(s.DB)
	config, err := json.Marshal(map[string]any{
		"url": server.URL, "template": tmpl,
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	_, _ = channelStore.CreateAlertChannel(ctx, &model.AlertChannel{
		Name: "Hook", ChannelType: "webhook", Config: string(config), Enabled: true,
	})

	eventStore := store.NewAlertEventStore(s.DB)
	_ = eventStore.CreateEvent(ctx, &model.AlertEvent{
		ID: "ev-tmpl", RuleID: rule.ID, NodeID: node.ID,
		Value: 53, Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
	})

	h := NewAlertHandler(ruleStore, channelStore, eventStore, nodeStore)
	req := httptest.NewRequest(http.MethodPut, "/api/alerts/events/ev-tmpl/acknowledge", nil)
	req.SetPathValue("id", "ev-tmpl")
	w := httptest.NewRecorder()
	h.HandleAcknowledgeAlertEvent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	if !strings.Contains(captured, "[warning] High Memory on edge-node: memory=53.00") {
		t.Fatalf("captured payload = %q, want rendered template with all placeholders", captured)
	}
	if !strings.Contains(captured, "acknowledged") {
		t.Fatalf("captured payload = %q, want status acknowledged", captured)
	}
}
