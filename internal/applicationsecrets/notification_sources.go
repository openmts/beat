package applicationsecrets

import (
	"context"
	"database/sql"

	"github.com/beat/backend/internal/secretbox"
)

func hasNotificationSchema(ctx context.Context, database databaseReader) (bool, error) {
	const requiredTables = 8
	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN (
		'notification_secret_state','alert_channel_revisions','notification_messages','notification_deliveries',
		'notification_delivery_attempts','notification_channel_health','notification_settings','notification_request_budgets')`).Scan(&count)
	if err != nil {
		return false, err
	}
	if count != 0 && count != requiredTables {
		return false, ErrUnhealthy
	}
	return count != 0, nil
}

func notificationSources() []resourceSource {
	return []resourceSource{
		{name: "notification_channel", query: resourceQuery("alert_channel_revisions", "id, channel_id, revision, channel_type, "+boundedColumn("secret_ciphertext"), oversizedColumn("secret_ciphertext")),
			update: "UPDATE alert_channel_revisions SET secret_ciphertext=? WHERE id=? AND secret_ciphertext IS ?", scan: scanNotificationChannel},
		{name: "notification_message", query: resourceQuery("notification_messages", "id, "+boundedColumn("payload_encrypted"), oversizedColumn("payload_encrypted")),
			update: "UPDATE notification_messages SET payload_encrypted=? WHERE id=? AND payload_encrypted IS ?", scan: scanNotificationMessage},
	}
}

func scanNotificationChannel(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var channelID, kind string
	var revision int64
	err := rows.Scan(&resource.id, &channelID, &revision, &kind, &resource.ciphertext, &resource.invalid)
	resource.required = true
	resource.aad = secretbox.NotificationChannelAAD(channelID, revision, kind)
	return resource, err
}

func scanNotificationMessage(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	err := rows.Scan(&resource.id, &resource.ciphertext, &resource.invalid)
	resource.required = true
	resource.aad = secretbox.NotificationMessageAAD(resource.id)
	return resource, err
}

func sourcesForDatabase(ctx context.Context, database databaseReader, legacySSH bool) ([]resourceSource, error) {
	sources := resourceSources(legacySSH)
	present, err := hasNotificationSchema(ctx, database)
	if err != nil {
		return nil, err
	}
	if present {
		sources = append(sources, notificationSources()...)
	}
	operations, err := operationSourcesForDatabase(ctx, database)
	if err != nil {
		return nil, err
	}
	sources = append(sources, operations...)
	geoip, err := geoIPSourcesForDatabase(ctx, database)
	if err != nil {
		return nil, err
	}
	sources = append(sources, geoip...)
	diagnostic, err := diagnosticSourcesForDatabase(ctx, database)
	if err != nil {
		return nil, err
	}
	return append(sources, diagnostic...), nil
}

func validateNotificationState(ctx context.Context, database databaseReader) error {
	present, err := hasNotificationSchema(ctx, database)
	if err != nil || !present {
		return err
	}
	var phase string
	if err := database.QueryRowContext(ctx, "SELECT phase FROM notification_secret_state WHERE id=1").Scan(&phase); err != nil {
		return err
	}
	var legacy int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('alert_channels') WHERE name='config'").Scan(&legacy); err != nil {
		return err
	}
	if phase != "ready" || legacy != 0 {
		return ErrUnhealthy
	}
	return nil
}
