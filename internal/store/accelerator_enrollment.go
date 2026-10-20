package store

import "context"

func applyAcceleratorEnrollmentPolicy(ctx context.Context, db schemaExecutor, nodeID string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO accelerator_collection_policies(scope_kind,scope_id,enabled,sample_interval_seconds,maximum_devices,metrics_json,revision,created_by,updated_by,updated_at)
 SELECT 'node',?,enabled,sample_interval_seconds,maximum_devices,metrics_json,1,created_by,updated_by,updated_at
 FROM accelerator_collection_policies WHERE scope_kind='enrollment_policy' AND scope_id='default'
 ON CONFLICT(scope_kind,scope_id) DO NOTHING`, nodeID)
	return err
}
