package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

var ErrPanelAlertRuleNotFound = errors.New("alert rule for the panel link was not found")

type DashboardPanelAlertStore struct{ db *sql.DB }

func NewDashboardPanelAlertStore(db *sql.DB) *DashboardPanelAlertStore {
	return &DashboardPanelAlertStore{db: db}
}

// List 返回看板内所有面板的规则关联，并附带聚合后的评估状态。
func (store *DashboardPanelAlertStore) List(ctx context.Context, dashboardID string) ([]model.DashboardPanelAlert, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT l.dashboard_id, l.panel_id, l.rule_id, l.created_at,
		COALESCE(r.name, ''), COALESCE(r.metric, ''), COALESCE(r.severity, ''), COALESCE(r.enabled, 0)
		FROM dashboard_panel_alerts l LEFT JOIN alert_rules r ON r.id = l.rule_id
		WHERE l.dashboard_id=? ORDER BY l.panel_id, l.rule_id`, dashboardID)
	if err != nil {
		return nil, fmt.Errorf("list dashboard panel alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := []model.DashboardPanelAlert{}
	for rows.Next() {
		var value model.DashboardPanelAlert
		if err := rows.Scan(&value.DashboardID, &value.PanelID, &value.RuleID, &value.CreatedAt,
			&value.RuleName, &value.Metric, &value.Severity, &value.Enabled); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attachPanelAlertStates(ctx, store.db, values)
}

// Replace 覆盖某个面板的规则关联；列表为空表示解除该面板的全部关联。
func (store *DashboardPanelAlertStore) Replace(ctx context.Context, dashboardID, panelID string, ruleIDs []string, actor DashboardActor) error {
	if err := validatePanelAlertRequest(dashboardID, panelID, ruleIDs); err != nil {
		return err
	}
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	authority, err := dashboardActorAuthority(ctx, transaction, actor)
	if err != nil {
		return err
	}
	if err := dashboardTargetExists(ctx, transaction, dashboardID); err != nil {
		return err
	}
	if err := requirePanelAlertRules(ctx, transaction, ruleIDs); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx,
		"DELETE FROM dashboard_panel_alerts WHERE dashboard_id=? AND panel_id=?", dashboardID, panelID); err != nil {
		return fmt.Errorf("clear dashboard panel alerts: %w", err)
	}
	for _, ruleID := range ruleIDs {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO dashboard_panel_alerts
			(dashboard_id,panel_id,rule_id,created_at,created_by) VALUES(?,?,?,?,?)`,
			dashboardID, panelID, ruleID, authority.now, actor.UserID); err != nil {
			return fmt.Errorf("link dashboard panel alert: %w", err)
		}
	}
	if err := panelAlertAudit(ctx, transaction, authority, actor, dashboardID, panelID, ruleIDs); err != nil {
		return err
	}
	return transaction.Commit()
}

func validatePanelAlertRequest(dashboardID, panelID string, ruleIDs []string) error {
	if strings.TrimSpace(dashboardID) == "" || len(dashboardID) > 80 || len(panelID) < 1 || len(panelID) > 64 {
		return model.ErrInvalidDashboard
	}
	if len(ruleIDs) > model.MaxPanelAlertRules {
		return model.ErrInvalidDashboard
	}
	seen := make(map[string]bool, len(ruleIDs))
	for _, id := range ruleIDs {
		if len(id) < 1 || len(id) > 64 || seen[id] {
			return model.ErrInvalidDashboard
		}
		seen[id] = true
	}
	return nil
}

func requirePanelAlertRules(ctx context.Context, database schemaExecutor, ruleIDs []string) error {
	if len(ruleIDs) == 0 {
		return nil
	}
	query := "SELECT COUNT(*) FROM alert_rules WHERE deleted_at IS NULL AND id IN (" + sqlPlaceholders(len(ruleIDs)) + ")"
	args := make([]any, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		args = append(args, id)
	}
	var count int
	if err := database.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPanelAlertRuleNotFound
		}
		return err
	}
	if count != len(ruleIDs) {
		return ErrPanelAlertRuleNotFound
	}
	return nil
}

func panelAlertAudit(ctx context.Context, database schemaExecutor, authority dashboardAuthority, actor DashboardActor, dashboardID, panelID string, ruleIDs []string) error {
	detail, err := json.Marshal(struct {
		PanelID string   `json:"panel_id"`
		RuleIDs []string `json:"rule_ids"`
	}{panelID, ruleIDs})
	if err != nil {
		return err
	}
	return insertAuditEvent(ctx, database, &model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: dashboardActorField(actor.RequestID, 128), ActorID: actor.UserID,
		ActorUsername: authority.username, SessionPrefix: authority.sessionPrefix,
		Action: "metrics.panel_alerts.update", ResourceType: "dashboard_panel_alert", ResourceID: dashboardID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail),
		IPAddress: dashboardActorField(actor.IPAddress, 128),
		UserAgent: dashboardActorField(actor.UserAgent, 512), CreatedAt: authority.now,
	})
}

// attachPanelAlertStates 用规则下所有节点的评估状态合成每个面板徽标。
func attachPanelAlertStates(ctx context.Context, database schemaExecutor, values []model.DashboardPanelAlert) ([]model.DashboardPanelAlert, error) {
	ruleIDs := make([]string, 0, len(values))
	for _, value := range values {
		ruleIDs = append(ruleIDs, value.RuleID)
	}
	states, err := readPanelAlertStates(ctx, database, ruleIDs)
	if err != nil {
		return nil, err
	}
	for index := range values {
		values[index].State = states[values[index].RuleID]
	}
	return values, nil
}

func readPanelAlertStates(ctx context.Context, database schemaExecutor, ruleIDs []string) (map[string]*model.DashboardPanelAlertStatus, error) {
	result := map[string]*model.DashboardPanelAlertStatus{}
	if len(ruleIDs) == 0 {
		return result, nil
	}
	query := `SELECT rule_id, phase, evaluation_status, reason_code, COUNT(*), MAX(updated_at)
		FROM alert_evaluation_states WHERE rule_id IN (` + sqlPlaceholders(len(ruleIDs)) + `)
		GROUP BY rule_id, phase, evaluation_status, reason_code`
	args := make([]any, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		args = append(args, id)
	}
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read dashboard panel alert states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ruleID, phase, status, reason string
		var count int
		var updated int64
		if err := rows.Scan(&ruleID, &phase, &status, &reason, &count, &updated); err != nil {
			return nil, err
		}
		mergePanelAlertState(result, ruleID, phase, status, reason, count, time.UnixMilli(updated).UTC())
	}
	return result, rows.Err()
}

func mergePanelAlertState(result map[string]*model.DashboardPanelAlertStatus, ruleID, phase, status, reason string, count int, updated time.Time) {
	state := result[ruleID]
	if state == nil {
		state = &model.DashboardPanelAlertStatus{Phase: model.AlertPhaseNormal}
		result[ruleID] = state
	}
	switch {
	case phase == model.AlertPhaseFiring:
		state.Firing += count
	case phase == model.AlertPhasePending:
		state.Pending += count
	case status == model.EvaluationNoData:
		state.NoData += count
	case status == model.EvaluationError:
		state.Error += count
	default:
		state.Normal += count
	}
	if model.PanelAlertPhaseRank(phase, status) < model.PanelAlertPhaseRank(state.Phase, state.Status) {
		return
	}
	state.Phase, state.Status, state.Reason = phase, status, reason
	at := updated.UTC()
	state.UpdatedAt = &at
}
