package applicationsecrets

import (
	"context"
	"database/sql"

	"github.com/beat/backend/internal/secretbox"
)

func geoIPSourcesForDatabase(ctx context.Context, db databaseReader) ([]resourceSource, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN('geoip_settings','node_locations','geoip_cache','lifecycle_settings','node_lifecycle','node_lifecycle_events')`).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	if count != 6 {
		return nil, ErrUnhealthy
	}
	return []resourceSource{
		{name: "geoip_source", query: resourceQuery("node_locations", "id,mode,"+boundedColumn("source_encrypted"), oversizedColumn("source_encrypted")), update: "UPDATE node_locations SET source_encrypted=? WHERE id=? AND source_encrypted IS ?", scan: scanGeoIPSource},
		{name: "geoip_credential", query: resourceQuery("geoip_settings", "id,"+boundedColumn("credential_encrypted"), oversizedColumn("credential_encrypted")), update: "UPDATE geoip_settings SET credential_encrypted=? WHERE id=? AND credential_encrypted IS ?", scan: scanGeoIPCredential},
	}, nil
}

func scanGeoIPSource(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	var mode string
	err := rows.Scan(&resource.id, &mode, &resource.ciphertext, &resource.invalid)
	resource.aad = secretbox.GeoIPSourceAAD(resource.id, mode)
	return resource, err
}

func scanGeoIPCredential(rows *sql.Rows) (secretResource, error) {
	var resource secretResource
	err := rows.Scan(&resource.id, &resource.ciphertext, &resource.invalid)
	resource.aad = secretbox.GeoIPCredentialAAD()
	return resource, err
}
