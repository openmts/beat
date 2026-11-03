package applicationsecrets

import (
	"context"
	"database/sql"

	"github.com/beat/backend/internal/secretbox"
)

func egressProxySourcesForDatabase(ctx context.Context, db databaseReader) ([]resourceSource, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='egress_proxy_settings'`).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return []resourceSource{{
		name:   "egress_proxy_secret",
		query:  resourceQuery("egress_proxy_settings", "id, revision, "+boundedColumn("proxy_secret_encrypted"), oversizedColumn("proxy_secret_encrypted")),
		update: "UPDATE egress_proxy_settings SET proxy_secret_encrypted=? WHERE id=? AND proxy_secret_encrypted IS ?",
		scan:   scanEgressProxySecret,
	}}, nil
}

func scanEgressProxySecret(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var revision int64
	err := rows.Scan(&resource.id, &revision, &resource.ciphertext, &resource.invalid)
	resource.aad = secretbox.EgressProxyAAD(revision)
	return resource, err
}
