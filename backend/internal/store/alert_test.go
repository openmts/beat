package store

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestListAlertRules(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	ruleStore := NewAlertRuleStore(store.DB)

	rule1 := &model.AlertRule{
		Name: "CPU Alert", Metric: "cpu", Operator: ">", Threshold: 90,
		Duration: 60, Severity: model.AlertSeverityCritical, Enabled: true,
	}
	_, err := ruleStore.CreateAlertRule(ctx, rule1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rule2 := &model.AlertRule{
		Name: "Memory Alert", Metric: "memory", Operator: ">", Threshold: 80,
		Duration: 30, Severity: model.AlertSeverityWarning, Enabled: true,
	}
	_, err = ruleStore.CreateAlertRule(ctx, rule2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rules, err := ruleStore.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}
}

func TestListAlertRulesEmpty(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	ruleStore := NewAlertRuleStore(store.DB)

	rules, err := ruleStore.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

func TestCreateAlertRule(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	ruleStore := NewAlertRuleStore(store.DB)

	rule := &model.AlertRule{
		Name: "Disk Alert", Metric: "disk", Operator: "<", Threshold: 10,
		Duration: 120, Severity: model.AlertSeverityInfo, Enabled: false,
	}
	created, err := ruleStore.CreateAlertRule(ctx, rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created == nil {
		t.Fatal("expected rule, got nil")
	}
	if created.Name != "Disk Alert" {
		t.Errorf("expected name %q, got %q", "Disk Alert", created.Name)
	}
	if created.Metric != "disk" {
		t.Errorf("expected metric %q, got %q", "disk", created.Metric)
	}
	if created.Operator != "<" {
		t.Errorf("expected operator %q, got %q", "<", created.Operator)
	}
	if created.Threshold != 10 {
		t.Errorf("expected threshold %f, got %f", 10.0, created.Threshold)
	}
	if created.Duration != 120 {
		t.Errorf("expected duration %d, got %d", 120, created.Duration)
	}
	if created.Severity != model.AlertSeverityInfo {
		t.Errorf("expected severity %q, got %q", model.AlertSeverityInfo, created.Severity)
	}
	if created.Enabled {
		t.Error("expected Enabled to be false")
	}
	if created.ID == "" {
		t.Error("expected non-empty rule ID")
	}
}

func TestUpdateAlertRule(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	ruleStore := NewAlertRuleStore(store.DB)

	rule := &model.AlertRule{
		Name: "Original", Metric: "cpu", Operator: ">", Threshold: 50,
		Duration: 10, Severity: model.AlertSeverityWarning, Enabled: true,
	}
	created, err := ruleStore.CreateAlertRule(ctx, rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &model.AlertRule{
		Name: "Updated", Metric: "memory", Operator: "<", Threshold: 20,
		Duration: 60, Severity: model.AlertSeverityCritical, Enabled: false,
	}
	result, err := ruleStore.UpdateAlertRule(ctx, created.ID, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected updated rule, got nil")
	}
	if result.Name != "Updated" {
		t.Errorf("expected name %q, got %q", "Updated", result.Name)
	}
	if result.Metric != "memory" {
		t.Errorf("expected metric %q, got %q", "memory", result.Metric)
	}
	if result.Enabled {
		t.Error("expected Enabled to be false after update")
	}
}

func TestDeleteAlertRule(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	ruleStore := NewAlertRuleStore(store.DB)

	rule := &model.AlertRule{
		Name: "To Delete", Metric: "cpu", Operator: ">", Threshold: 90,
		Duration: 60, Severity: model.AlertSeverityWarning, Enabled: true,
	}
	created, err := ruleStore.CreateAlertRule(ctx, rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ruleStore.DeleteAlertRule(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rules, err := ruleStore.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestListEnabledRules(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	ruleStore := NewAlertRuleStore(store.DB)

	enabled := &model.AlertRule{
		Name: "Enabled Rule", Metric: "cpu", Operator: ">", Threshold: 90,
		Duration: 60, Severity: model.AlertSeverityCritical, Enabled: true,
	}
	_, err := ruleStore.CreateAlertRule(ctx, enabled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disabled := &model.AlertRule{
		Name: "Disabled Rule", Metric: "memory", Operator: "<", Threshold: 10,
		Duration: 30, Severity: model.AlertSeverityInfo, Enabled: false,
	}
	_, err = ruleStore.CreateAlertRule(ctx, disabled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	enabledRules, err := ruleStore.ListEnabledRules(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(enabledRules) != 1 {
		t.Errorf("expected 1 enabled rule, got %d", len(enabledRules))
	}
	if enabledRules[0].Name != "Enabled Rule" {
		t.Errorf("expected name %q, got %q", "Enabled Rule", enabledRules[0].Name)
	}
}

func TestListAlertChannels(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	channelStore := NewAlertChannelStore(store.DB)

	ch1 := &model.AlertChannel{
		Name: "Email", ChannelType: "email", Config: `{"to":"a@b.com"}`, Enabled: true,
	}
	_, err := channelStore.CreateAlertChannel(ctx, ch1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch2 := &model.AlertChannel{
		Name: "Slack", ChannelType: "slack", Config: `{"url":"https://hook"}`, Enabled: true,
	}
	_, err = channelStore.CreateAlertChannel(ctx, ch2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	channels, err := channelStore.ListAlertChannels(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}
}

func TestCreateAlertChannel(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	channelStore := NewAlertChannelStore(store.DB)

	ch := &model.AlertChannel{
		Name: "Webhook", ChannelType: "webhook", Config: `{"url":"https://example.com"}`, Enabled: false,
	}
	created, err := channelStore.CreateAlertChannel(ctx, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created == nil {
		t.Fatal("expected channel, got nil")
	}
	if created.Name != "Webhook" {
		t.Errorf("expected name %q, got %q", "Webhook", created.Name)
	}
	if created.ChannelType != "webhook" {
		t.Errorf("expected channel_type %q, got %q", "webhook", created.ChannelType)
	}
	if created.Config != `{"url":"https://example.com"}` {
		t.Errorf("expected config %q, got %q", `{"url":"https://example.com"}`, created.Config)
	}
	if created.Enabled {
		t.Error("expected Enabled to be false")
	}
	if created.ID == "" {
		t.Error("expected non-empty channel ID")
	}
}

func TestUpdateAlertChannel(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	channelStore := NewAlertChannelStore(store.DB)

	ch := &model.AlertChannel{
		Name: "Original", ChannelType: "email", Config: `{"to":"old@b.com"}`, Enabled: true,
	}
	created, err := channelStore.CreateAlertChannel(ctx, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &model.AlertChannel{
		Name: "Updated", ChannelType: "slack", Config: `{"url":"https://new-hook"}`, Enabled: false,
	}
	result, err := channelStore.UpdateAlertChannel(ctx, created.ID, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected updated channel, got nil")
	}
	if result.Name != "Updated" {
		t.Errorf("expected name %q, got %q", "Updated", result.Name)
	}
	if result.ChannelType != "slack" {
		t.Errorf("expected channel_type %q, got %q", "slack", result.ChannelType)
	}
	if result.Enabled {
		t.Error("expected Enabled to be false after update")
	}
}

func TestDeleteAlertChannel(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	channelStore := NewAlertChannelStore(store.DB)

	ch := &model.AlertChannel{
		Name: "To Delete", ChannelType: "email", Config: `{"to":"x@y.com"}`, Enabled: true,
	}
	created, err := channelStore.CreateAlertChannel(ctx, ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = channelStore.DeleteAlertChannel(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	channels, err := channelStore.ListAlertChannels(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels after delete, got %d", len(channels))
	}
}

func TestListEnabledChannels(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	channelStore := NewAlertChannelStore(store.DB)

	enabled := &model.AlertChannel{
		Name: "Enabled Channel", ChannelType: "email", Config: `{"to":"a@b.com"}`, Enabled: true,
	}
	_, err := channelStore.CreateAlertChannel(ctx, enabled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disabled := &model.AlertChannel{
		Name: "Disabled Channel", ChannelType: "slack", Config: `{"url":"https://hook"}`, Enabled: false,
	}
	_, err = channelStore.CreateAlertChannel(ctx, disabled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	enabledChannels, err := channelStore.ListEnabledChannels(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(enabledChannels) != 1 {
		t.Errorf("expected 1 enabled channel, got %d", len(enabledChannels))
	}
	if enabledChannels[0].Name != "Enabled Channel" {
		t.Errorf("expected name %q, got %q", "Enabled Channel", enabledChannels[0].Name)
	}
}

func TestListAlertEvents(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	eventStore := NewAlertEventStore(store.DB)

	now := model.NowUTC()
	event1 := &model.AlertEvent{
		ID: "event-1", RuleID: "rule-1", NodeID: "node-1",
		Message: "CPU high", Value: 95.5, Status: model.AlertStatusTriggered,
		TriggeredAt: now,
	}
	err := eventStore.CreateEvent(ctx, event1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event2 := &model.AlertEvent{
		ID: "event-2", RuleID: "rule-2", NodeID: "node-2",
		Message: "Memory low", Value: 5.0, Status: model.AlertStatusTriggered,
		TriggeredAt: now.Add(time.Hour),
	}
	err = eventStore.CreateEvent(ctx, event2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, err := eventStore.ListAlertEvents(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestCreateEvent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	eventStore := NewAlertEventStore(store.DB)

	now := model.NowUTC()
	event := &model.AlertEvent{
		ID: "event-create", RuleID: "rule-x", NodeID: "node-x",
		Message: "Disk full", Value: 99.0, Status: model.AlertStatusTriggered,
		TriggeredAt: now,
	}
	err := eventStore.CreateEvent(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, err := eventStore.ListAlertEvents(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != "event-create" {
		t.Errorf("expected ID %q, got %q", "event-create", events[0].ID)
	}
	if events[0].Message != "Disk full" {
		t.Errorf("expected message %q, got %q", "Disk full", events[0].Message)
	}
	if events[0].Value != 99.0 {
		t.Errorf("expected value %f, got %f", 99.0, events[0].Value)
	}
}

func TestGetActiveEvent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	eventStore := NewAlertEventStore(store.DB)

	now := model.NowUTC()
	triggered := &model.AlertEvent{
		ID: "active-1", RuleID: "rule-active", NodeID: "node-active",
		Message: "CPU critical", Value: 99.0, Status: model.AlertStatusTriggered,
		TriggeredAt: now,
	}
	err := eventStore.CreateEvent(ctx, triggered)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active, err := eventStore.GetActiveEvent(ctx, "rule-active", "node-active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active == nil {
		t.Fatal("expected active event, got nil")
	}
	if active.ID != "active-1" {
		t.Errorf("expected ID %q, got %q", "active-1", active.ID)
	}

	resolved := &model.AlertEvent{
		ID: "resolved-1", RuleID: "rule-active", NodeID: "node-active",
		Message: "CPU resolved", Value: 50.0, Status: model.AlertStatusResolved,
		TriggeredAt: now,
	}
	err = eventStore.CreateEvent(ctx, resolved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active, err = eventStore.GetActiveEvent(ctx, "rule-active", "node-active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active == nil {
		t.Fatal("expected active event still found, got nil")
	}

	active, err = eventStore.GetActiveEvent(ctx, "rule-nonexist", "node-nonexist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active != nil {
		t.Error("expected nil for non-existent active event")
	}
}

func TestUpdateEvent(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	eventStore := NewAlertEventStore(store.DB)

	now := model.NowUTC()
	event := &model.AlertEvent{
		ID: "event-update", RuleID: "rule-u", NodeID: "node-u",
		Message: "Alert", Value: 80.0, Status: model.AlertStatusTriggered,
		TriggeredAt: now,
	}
	err := eventStore.CreateEvent(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolvedAt := model.NowUTC()
	event.Status = model.AlertStatusResolved
	event.ResolvedAt = &resolvedAt
	err = eventStore.UpdateEvent(ctx, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, err := eventStore.ListAlertEvents(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Status != model.AlertStatusResolved {
		t.Errorf("expected status %q, got %q", model.AlertStatusResolved, events[0].Status)
	}
}
