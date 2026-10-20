import { useLocale } from "@/context/locale"

// 看板协作面文案位于 metric-workspace 目录的 workspace.collab.* 键；调用点键名保持不变。
export type CollaborationTextKey = "annotations" | "add_annotation" | "annotation_title" | "annotation_text" | "annotation_tags" | "annotation_color" | "annotation_visibility" | "annotation_private" | "annotation_public" | "annotation_window" | "annotation_scope" | "annotation_scope_dashboard" | "annotation_created" | "delete_annotation" | "no_annotations" | "snapshots" | "snapshot_description" | "snapshot_name" | "create_snapshot" | "snapshot_created" | "snapshot_expiry" | "snapshot_expiry_none" | "snapshot_expiry_day" | "snapshot_expiry_week" | "revoke_snapshot" | "no_snapshots" | "snapshot_view_title" | "snapshot_frozen_note" | "snapshot_missing" | "panel_library" | "save_to_library" | "insert_from_library" | "panel_library_name" | "panel_library_saved" | "no_library_panels" | "search" | "no_library_match" | "insert" | "panel_alerts" | "link_alerts" | "alert_linked" | "no_alert_rules" | "alert_state_firing" | "alert_state_pending" | "alert_state_recovering" | "alert_state_no_data" | "alert_state_error" | "alert_state_normal" | "view_alert_history" | "save_failed" | "load_failed" | "copy_link" | "cancel" | "save" | "close" | "delete"

export function useCollaborationText(): (key: CollaborationTextKey) => string {
  const { t } = useLocale()
  return (key) => t(`workspace.collab.${key}`)
}

/** 面板徽标只用一个词描述聚合后的最严重状态。 */
export function alertStateKey(phase: string, status: string): CollaborationTextKey {
  if (phase === "firing") return "alert_state_firing"
  if (phase === "pending") return "alert_state_pending"
  if (phase === "recovering") return "alert_state_recovering"
  if (status === "no_data") return "alert_state_no_data"
  if (status === "error") return "alert_state_error"
  return "alert_state_normal"
}

/** 面板徽标下钻到该规则的评估历史；规则契约由告警模块拥有，这里只生成链接。 */
export function alertHistoryLink(ruleId: string): string {
  return `/admin/alerts?tab=evaluation-history&rule_id=${encodeURIComponent(ruleId)}`
}
