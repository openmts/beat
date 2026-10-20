package fleetsummary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

const summaryBuildTimeout = 30 * time.Second

// RunDue 逐个处理到期计划；单个计划失败不影响其他计划，错误聚合返回。
func (s *Service) RunDue(ctx context.Context) error {
	now := s.now()
	schedules, err := s.schedules.ListDue(ctx, now)
	if err != nil {
		return fmt.Errorf("list due fleet summary schedules: %w", err)
	}
	var failures error
	for _, schedule := range schedules {
		if err := s.runScheduled(ctx, schedule, now); err != nil {
			failures = errors.Join(failures, fmt.Errorf("run fleet summary schedule %s: %w", schedule.ID, err))
		}
	}
	return failures
}

func (s *Service) runScheduled(
	ctx context.Context,
	schedule model.FleetSummarySchedule,
	now time.Time,
) error {
	if s.queue == nil {
		return notificationqueue.ErrQueueUnavailable
	}
	if schedule.NextRunAt == nil {
		return errors.New("fleet summary schedule is missing its next run time")
	}
	dueAt := *schedule.NextRunAt
	periodKey, err := PeriodKey(schedule, dueAt)
	if err != nil {
		return err
	}
	// 补跑语义：每次只结算一个缺失周期，并把游标推进到下一个未来时刻。
	reference := now
	if dueAt.After(reference) {
		reference = dueAt
	}
	next, err := NextRun(schedule, reference)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, summaryBuildTimeout)
	defer cancel()
	counts, groupNames, err := s.snapshot(ctx, schedule)
	if err != nil {
		return err
	}
	request, err := summaryRequest(schedule, dueAt, periodKey, counts, groupNames)
	if err != nil {
		return err
	}
	completion := store.FleetSummaryCompletion{
		Schedule: schedule, PeriodKey: periodKey, NextRun: next, RunAt: now,
		Run: model.FleetSummaryRun{
			ScheduleID: schedule.ID, ScheduleName: schedule.Name, Cadence: schedule.Cadence,
			Timezone: schedule.Timezone, Kind: model.FleetSummaryRunScheduled,
			Counts: counts, ChannelCount: len(schedule.ChannelIDs), CreatedAt: now,
		},
	}
	_, err = s.queue.Enqueue(ctx, request, func(transaction *sql.Tx) error {
		changed, err := s.schedules.CompleteRunTx(ctx, transaction, completion)
		if err != nil {
			return err
		}
		if !changed {
			return notificationqueue.ErrSkipMutation
		}
		return nil
	})
	return err
}

// snapshot 读取作用域内节点的已持久化可用性状态，并按阈值截断名称列表。
func (s *Service) snapshot(
	ctx context.Context,
	schedule model.FleetSummarySchedule,
) (model.FleetSummaryCounts, []string, error) {
	nodes, err := s.nodes.ListNodes(ctx, "")
	if err != nil {
		return model.FleetSummaryCounts{}, nil, fmt.Errorf("list fleet nodes: %w", err)
	}
	groupNames, err := s.groupNames(ctx, schedule.GroupIDs)
	if err != nil {
		return model.FleetSummaryCounts{}, nil, err
	}
	counts := model.FleetSummaryCounts{OnlineNames: []string{}, OfflineNames: []string{}}
	for _, node := range selectScopedNodes(nodes, schedule) {
		name := nodeDisplayName(node)
		if node.Status == model.NodeStatusOnline {
			counts.Online++
			counts.OnlineNames = append(counts.OnlineNames, name)
			continue
		}
		counts.Offline++
		counts.OfflineNames = append(counts.OfflineNames, name)
	}
	counts.Total = counts.Online + counts.Offline
	return counts, groupNames, nil
}

func (s *Service) groupNames(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 || s.groups == nil {
		return []string{}, nil
	}
	groups, err := s.groups.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fleet groups: %w", err)
	}
	names := make(map[string]string, len(groups))
	for _, group := range groups {
		names[group.ID] = group.Name
	}
	selected := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := strings.TrimSpace(names[id]); name != "" {
			selected = append(selected, name)
		}
	}
	return selected, nil
}

func selectScopedNodes(nodes []model.Node, schedule model.FleetSummarySchedule) []model.Node {
	if len(schedule.GroupIDs) == 0 && len(schedule.NodeIDs) == 0 {
		return nodes
	}
	groups := make(map[string]struct{}, len(schedule.GroupIDs))
	for _, id := range schedule.GroupIDs {
		groups[id] = struct{}{}
	}
	direct := make(map[string]struct{}, len(schedule.NodeIDs))
	for _, id := range schedule.NodeIDs {
		direct[id] = struct{}{}
	}
	selected := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if _, found := direct[node.ID]; found {
			selected = append(selected, node)
			continue
		}
		if _, found := groups[node.GroupID]; found {
			selected = append(selected, node)
		}
	}
	return selected
}

func nodeDisplayName(node model.Node) string {
	if alias := strings.TrimSpace(node.Alias); alias != "" {
		return alias
	}
	return node.Name
}

func summarySourceKey(scheduleID, periodKey string) string {
	return "fleet_summary:" + scheduleID + ":" + periodKey
}

func summaryRequest(
	schedule model.FleetSummarySchedule,
	cutoff time.Time,
	periodKey string,
	counts model.FleetSummaryCounts,
	groupNames []string,
) (notificationqueue.Request, error) {
	text := summaryText(schedule, cutoff, periodKey, counts, groupNames)
	if len(text) > notificationqueue.MaximumTextBytes {
		return notificationqueue.Request{}, ErrSummaryTooLarge
	}
	subject := "[Beat] Fleet status summary: " + schedule.Name
	data := map[string]string{
		"fleet.schedule_name": schedule.Name,
		"fleet.period_key":    periodKey,
		"fleet.cutoff":        cutoff.UTC().Format(time.RFC3339),
		"fleet.online_count":  fmt.Sprintf("%d", counts.Online),
		"fleet.offline_count": fmt.Sprintf("%d", counts.Offline),
		"fleet.total_count":   fmt.Sprintf("%d", counts.Total),
		"fleet.online_names":  nameList(counts.OnlineNames, schedule.NameThreshold),
		"fleet.offline_names": nameList(counts.OfflineNames, schedule.NameThreshold),
		"fleet.group_names":   strings.Join(groupNames, ", "),
		"event.message":       text,
		"event.occurred_at":   cutoff.UTC().Format(time.RFC3339Nano),
	}
	return notificationqueue.Request{
		SourceKey: summarySourceKey(schedule.ID, periodKey), SourceKind: "fleet_summary",
		SourceID: schedule.ID, EventKind: "fleet.status_summary", Subject: subject, Text: text,
		Data: data, ChannelIDs: append([]string(nil), schedule.ChannelIDs...),
	}, nil
}

func summaryText(
	schedule model.FleetSummarySchedule,
	cutoff time.Time,
	periodKey string,
	counts model.FleetSummaryCounts,
	groupNames []string,
) string {
	lines := []string{
		"Beat fleet status summary",
		"Schedule: " + schedule.Name,
		"Period: " + periodKey + " (" + schedule.Timezone + ")",
		"Cutoff: " + cutoff.UTC().Format(time.RFC3339),
	}
	if len(groupNames) > 0 {
		lines = append(lines, "Groups: "+strings.Join(groupNames, ", "))
	}
	lines = append(lines,
		fmt.Sprintf("Nodes: %d total, %d online, %d offline", counts.Total, counts.Online, counts.Offline),
		"Online: "+nameList(counts.OnlineNames, schedule.NameThreshold),
		"Offline: "+nameList(counts.OfflineNames, schedule.NameThreshold),
	)
	return strings.Join(lines, "\n")
}

// nameList 只在名称数量不超过阈值时展开；阈值为 0 表示永不展开。
func nameList(names []string, threshold int) string {
	if len(names) == 0 {
		return "none"
	}
	if len(names) <= threshold {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%d nodes (names omitted)", len(names))
}

func (s *Service) hydrateDelivery(ctx context.Context, schedule *model.FleetSummarySchedule) error {
	if schedule.LastPeriodKey == "" || s.queue == nil {
		return nil
	}
	receipt, err := s.queue.SourceReceipt(ctx, summarySourceKey(schedule.ID, schedule.LastPeriodKey))
	if errors.Is(err, notificationqueue.ErrNotFound) {
		schedule.LastResult = resultPointer(model.FleetSummaryResultUnknown)
		schedule.DeliveryState = resultPointer(model.FleetSummaryResultUnknown)
		return nil
	}
	if err != nil {
		return fmt.Errorf("read fleet summary delivery receipt: %w", err)
	}
	result, state := deliveryOutcome(receipt)
	schedule.LastResult = &result
	schedule.DeliveryState = &state
	return nil
}

// deliveryOutcome 把 outbox 回执折叠为计划行的最近结果与投递状态。
func deliveryOutcome(receipt notificationqueue.Receipt) (string, string) {
	counts := receipt.Counts
	if receipt.RoutingState == "not_routed" {
		return model.FleetSummaryResultFailed, model.FleetSummaryResultFailed
	}
	switch {
	case counts.Pending > 0 || counts.Leased > 0:
		return model.FleetSummaryResultQueued, model.FleetSummaryResultQueued
	case counts.Dead > 0 && counts.Succeeded > 0:
		return model.FleetSummaryResultPartial, model.FleetSummaryResultPartial
	case counts.Dead > 0:
		return model.FleetSummaryResultFailed, model.FleetSummaryResultFailed
	case counts.Succeeded > 0:
		return model.FleetSummaryResultSuccess, "succeeded"
	default:
		return model.FleetSummaryResultUnknown, model.FleetSummaryResultUnknown
	}
}

func resultPointer(value string) *string { return &value }
