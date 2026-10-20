package remoteoperations

func (tx transaction) fileDispatchAllowed(taskID string) error {
	var direction, state, scan string
	var required bool
	var expiry int64
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT f.direction,COALESCE(a.state,''),COALESCE(a.expires_at,0),COALESCE(a.scan_status,''),(f.require_scan OR COALESCE(json_extract(p.configuration_json,'$.require_scan'),0))
 FROM operation_transfers f LEFT JOIN operation_artifacts a ON a.id=f.artifact_id LEFT JOIN operation_policies p ON p.id=f.policy_id WHERE f.task_id=?`, taskID).Scan(&direction, &state, &expiry, &scan, &required)
	if err != nil {
		return ErrUnavailable
	}
	if expiry <= tx.now.UnixMilli() {
		return ErrConflict
	}
	if direction == "deploy" && (state != "ready" || required && scan != "clean") {
		return ErrConflict
	}
	if direction == "collect" && state != "ready" && state != "uploading" && state != "awaiting_source" {
		return ErrConflict
	}
	return nil
}
