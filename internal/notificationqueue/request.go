package notificationqueue

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumSubjectBytes      = 200
	MaximumTextBytes         = 16 * 1024
	MaximumPayloadBytes      = 64 * 1024
	MaximumChannels          = 256
	MaximumPendingDeliveries = 100000
)

var enabledEventKinds = map[string]bool{
	"alert.triggered": true, "alert.acknowledged": true, "alert.resolved": true,
	"alert.no_data": true, "alert.evaluation_error": true, "alert.evaluation_recovered": true,
	"availability.offline": true, "availability.recovered": true, "traffic.quota": true, "traffic.report": true,
	"fleet.status_summary":         true,
	"notification.delivery_failed": true, "notification.test": true,
	"operation.completed": true, "operation.failed": true,
	"node.expiring": true, "node.expired": true, "node.renewed": true,
	"security.login_succeeded": true, "security.login_suspicious": true,
}

var templateDataKeys = map[string]bool{
	"event.kind": true, "event.status": true, "event.severity": true, "event.message": true, "event.occurred_at": true,
	"node.id": true, "node.name": true, "node.group_name": true, "node.labels": true,
	"rule.id": true, "rule.name": true, "metric.name": true, "metric.value": true, "metric.formatted_value": true,
	"site.title": true, "site.public_base_url": true, "security.authentication_method": true,
	"security.source_summary": true, "security.location_summary": true, "security.device_summary": true,
	"security.administrator": true, "security.reason_summary": true,
	"delivery.id": true, "delivery.attempt": true,
	"evaluation.status": true, "evaluation.reason": true, "evaluation.epoch": true,
	"fleet.schedule_name": true, "fleet.period_key": true, "fleet.cutoff": true,
	"fleet.online_count": true, "fleet.offline_count": true, "fleet.total_count": true,
	"fleet.online_names": true, "fleet.offline_names": true, "fleet.group_names": true,
}

func validateRequest(request Request) error {
	for _, field := range []struct {
		value string
		limit int
	}{{request.SourceKey, 512}, {request.SourceKind, 64}, {request.SourceID, 128}} {
		if field.value == "" || len(field.value) > field.limit || !cleanScalar(field.value) {
			return ErrInvalidRequest
		}
	}
	if !enabledEventKinds[request.EventKind] || len(request.Subject) > MaximumSubjectBytes || !cleanScalar(request.Subject) {
		return ErrInvalidRequest
	}
	if len(request.Text) > MaximumTextBytes || !utf8.ValidString(request.Text) || strings.ContainsRune(request.Text, 0) {
		return ErrInvalidRequest
	}
	if len(request.ChannelIDs) > MaximumChannels || len(request.Data) > len(templateDataKeys) {
		return ErrInvalidRequest
	}
	if request.UseSubscriptions && len(request.ChannelIDs) > 0 {
		return ErrInvalidRequest
	}
	for _, id := range request.ChannelIDs {
		if id == "" || len(id) > 128 || !cleanScalar(id) {
			return ErrInvalidRequest
		}
	}
	for key, value := range request.Data {
		if !templateDataKeys[key] || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return ErrInvalidRequest
		}
	}
	encoded, err := json.Marshal(request.Data)
	if err != nil || len(encoded) > MaximumPayloadBytes-MaximumTextBytes-MaximumSubjectBytes {
		return ErrInvalidRequest
	}
	return nil
}

func cleanScalar(value string) bool {
	return utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}
