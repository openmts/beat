package trafficreport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
)

func (s *Service) buildReport(
	ctx context.Context,
	schedule model.TrafficReportSchedule,
	period model.TrafficReportPeriod,
) (model.TrafficReport, error) {
	nodes, err := s.nodes.ListNodes(ctx, "")
	if err != nil {
		return model.TrafficReport{}, fmt.Errorf("list report nodes: %w", err)
	}
	nodes = selectedNodes(nodes, schedule)
	reportNodes := make([]model.TrafficReportNode, 0, len(nodes))
	for _, node := range nodes {
		reportNode, err := s.buildReportNode(ctx, node, period)
		if err != nil {
			return model.TrafficReport{}, err
		}
		reportNodes = append(reportNodes, reportNode)
	}
	return model.TrafficReport{
		ScheduleID: schedule.ID, ScheduleName: schedule.Name,
		Cadence: schedule.Cadence, Timezone: schedule.Timezone,
		Period: period, GeneratedAt: s.now(), Nodes: reportNodes,
	}, nil
}

func (s *Service) buildReportNode(
	ctx context.Context,
	node model.Node,
	period model.TrafficReportPeriod,
) (model.TrafficReportNode, error) {
	totals, err := s.metrics.QueryTrafficUsage(ctx, node.ID, period.Start, period.End)
	if err != nil {
		return model.TrafficReportNode{}, fmt.Errorf("query traffic for node %s: %w", node.ID, err)
	}
	limitType := node.TrafficLimitType
	if limitType == "" {
		limitType = model.TrafficLimitSum
	}
	resetDay := node.TrafficResetDay
	if resetDay == 0 {
		resetDay = 1
	}
	summary, err := model.SummarizeTraffic(
		model.TrafficPolicy{LimitType: limitType, ResetDay: resetDay}, totals, period.End,
	)
	if err != nil {
		return model.TrafficReportNode{}, fmt.Errorf("summarize traffic for node %s: %w", node.ID, err)
	}
	return model.TrafficReportNode{
		ID: node.ID, Name: node.Name, Alias: node.Alias,
		Sent: summary.Sent, Received: summary.Received, Used: summary.Used,
		LimitType: summary.LimitType,
	}, nil
}

func (s *Service) deliver(
	ctx context.Context,
	schedule model.TrafficReportSchedule,
	report model.TrafficReport,
) (model.TrafficReportDeliveryStatus, error) {
	channels, err := s.channels.ListAlertChannels(ctx)
	if err != nil {
		return failedDeliveryStatus(s.now(), "failed to list notification channels"), err
	}
	channels = selectedChannels(channels, schedule)
	if len(channels) == 0 {
		status := failedDeliveryStatus(s.now(), "no enabled channels in schedule scope")
		return status, errors.New(status.Message)
	}
	message, err := reportMessage(report)
	if err != nil {
		return failedDeliveryStatus(s.now(), "failed to format traffic report"), err
	}
	delivered := 0
	deliveryErrors := make([]error, 0)
	for index := range channels {
		if _, err := s.delivery.SendMessage(ctx, &channels[index], message); err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("deliver to channel %s: %w", channels[index].ID, err))
			continue
		}
		delivered++
	}
	status := deliveryStatus(s.now(), delivered, len(channels))
	return status, errors.Join(deliveryErrors...)
}

func selectedNodes(nodes []model.Node, schedule model.TrafficReportSchedule) []model.Node {
	if schedule.AllNodes {
		return nodes
	}
	selected := stringSet(schedule.NodeIDs)
	result := make([]model.Node, 0, len(selected))
	for _, node := range nodes {
		if _, found := selected[node.ID]; found {
			result = append(result, node)
		}
	}
	return result
}

func selectedChannels(
	channels []model.AlertChannel,
	schedule model.TrafficReportSchedule,
) []model.AlertChannel {
	selected := stringSet(schedule.ChannelIDs)
	result := make([]model.AlertChannel, 0, len(channels))
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		if schedule.AllChannels {
			result = append(result, channel)
			continue
		}
		if _, found := selected[channel.ID]; found {
			result = append(result, channel)
		}
	}
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func deliveryStatus(now time.Time, delivered int, total int) model.TrafficReportDeliveryStatus {
	state := model.TrafficReportDeliverySuccess
	if delivered == 0 {
		state = model.TrafficReportDeliveryFailed
	} else if delivered < total {
		state = model.TrafficReportDeliveryPartial
	}
	return model.TrafficReportDeliveryStatus{
		State: state, Message: fmt.Sprintf("delivered to %d/%d channels", delivered, total),
		Delivered: delivered, Total: total, DeliveredAt: now,
	}
}

func reportMessage(report model.TrafficReport) (notification.Message, error) {
	location, err := time.LoadLocation(report.Timezone)
	if err != nil {
		return notification.Message{}, fmt.Errorf("load report timezone: %w", err)
	}
	period := fmt.Sprintf(
		"%s - %s (%s)",
		report.Period.Start.In(location).Format("2006-01-02 15:04"),
		report.Period.End.In(location).Format("2006-01-02 15:04"), report.Timezone,
	)
	lines := []string{"Beat traffic report", "Schedule: " + report.ScheduleName, "Period: " + period}
	if len(report.Nodes) == 0 {
		lines = append(lines, "Nodes: no nodes in scope")
	}
	for _, node := range report.Nodes {
		lines = append(lines, fmt.Sprintf(
			"%s: up %s, down %s, used %s (%s)", reportNodeName(node),
			formatBytes(node.Sent), formatBytes(node.Received), formatBytes(node.Used), node.LimitType,
		))
	}
	return notification.Message{
		Kind: "traffic_report", Subject: "[Beat] " + cadenceLabel(report.Cadence) + " traffic report",
		Text: strings.Join(lines, "\n"), Data: report,
	}, nil
}

func reportNodeName(node model.TrafficReportNode) string {
	if node.Alias != "" {
		return node.Alias + " (" + node.Name + ")"
	}
	return node.Name
}

func cadenceLabel(cadence string) string {
	switch cadence {
	case model.TrafficReportDaily:
		return "Daily"
	case model.TrafficReportWeekly:
		return "Weekly"
	case model.TrafficReportMonthly:
		return "Monthly"
	default:
		return "Scheduled"
	}
}

func formatBytes(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%.0f %s", value, units[index])
	}
	return fmt.Sprintf("%.2f %s", value, units[index])
}
