package store

import "context"

func MigrateAccelerators(ctx context.Context, db schemaExecutor) error {
	for _, statement := range acceleratorSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := reconcileMetricCatalog(ctx, db); err != nil {
		return err
	}
	return migrateAcceleratorAlerts(ctx, db)
}

var acceleratorSchema = []string{
	`CREATE TABLE node_accelerators(
 node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),
 device_id TEXT NOT NULL CHECK(length(device_id)=32),ordinal INTEGER NOT NULL CHECK(ordinal>0),vendor TEXT NOT NULL CHECK(vendor IN('nvidia','amd')),
 model TEXT NOT NULL CHECK(length(CAST(model AS BLOB)) BETWEEN 1 AND 128),driver_version TEXT NOT NULL DEFAULT '',
 capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),error_codes_json TEXT NOT NULL DEFAULT '[]',
 first_seen_at DATETIME NOT NULL,last_seen_at DATETIME NOT NULL,inactive_at DATETIME,
 PRIMARY KEY(node_id,fingerprint),UNIQUE(node_id,device_id),UNIQUE(node_id,ordinal))`,
	`CREATE INDEX idx_node_accelerators_active ON node_accelerators(node_id,inactive_at,ordinal)`,
	`CREATE TABLE node_accelerator_state(
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,sample_id TEXT NOT NULL,captured_at DATETIME NOT NULL,
 payload_digest TEXT NOT NULL CHECK(length(payload_digest)=64),inventory_json TEXT NOT NULL CHECK(json_valid(inventory_json) AND length(CAST(inventory_json AS BLOB))<=65536),
 accepted_at DATETIME NOT NULL)`,
	`CREATE TABLE accelerator_collection_policies(
 scope_kind TEXT NOT NULL CHECK(scope_kind IN('node','enrollment_policy')),scope_id TEXT NOT NULL,
 enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),sample_interval_seconds INTEGER NOT NULL CHECK(sample_interval_seconds BETWEEN 5 AND 3600),
 maximum_devices INTEGER NOT NULL CHECK(maximum_devices BETWEEN 1 AND 32),metrics_json TEXT NOT NULL CHECK(json_valid(metrics_json) AND length(metrics_json)<=512),
 revision INTEGER NOT NULL CHECK(revision>0),created_by TEXT NOT NULL,updated_by TEXT NOT NULL,updated_at DATETIME NOT NULL,
 PRIMARY KEY(scope_kind,scope_id))`,
	`CREATE TABLE accelerator_ingest_pending(
 node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,report_id TEXT NOT NULL,sample_id TEXT NOT NULL,
 captured_at DATETIME NOT NULL,payload_digest TEXT NOT NULL CHECK(length(payload_digest)=64),
 inventory_json TEXT NOT NULL CHECK(json_valid(inventory_json) AND length(CAST(inventory_json AS BLOB))<=65536),
 credential_fingerprint TEXT NOT NULL,created_at DATETIME NOT NULL)`,
	`CREATE INDEX idx_accelerator_ingest_pending ON accelerator_ingest_pending(created_at,node_id)`,
	`CREATE TRIGGER accelerator_node_deleted AFTER DELETE ON nodes BEGIN DELETE FROM accelerator_collection_policies WHERE scope_kind='node' AND scope_id=OLD.id;END`,
}
