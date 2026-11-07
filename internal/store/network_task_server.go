package store

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
)

// ListServerTasks 返回由 Beat Server 主动执行的启用任务及其节点地址。
// 探测目标在调度器中通过节点 host/port 渲染，避免把任意目标下发给服务端扫描。
func (s *NetworkTaskStore) ListServerTasks(ctx context.Context) ([]model.ServerNetworkTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, type, target, ip_family,
		interval_seconds, timeout_milliseconds, packet_count, all_nodes
		FROM network_tasks WHERE lifecycle_state = 'active' AND enabled = 1 AND direction = 'server_to_agent'
		ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("query server network tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	tasks := []model.ServerNetworkTask{}
	allNodes := []bool{}
	for rows.Next() {
		var task model.ServerNetworkTask
		var taskAllNodes bool
		if err := rows.Scan(&task.ID, &task.Name, &task.Type, &task.Target, &task.IPFamily,
			&task.IntervalSeconds, &task.TimeoutMilliseconds, &task.PacketCount, &taskAllNodes); err != nil {
			return nil, fmt.Errorf("scan server network task: %w", err)
		}
		tasks = append(tasks, task)
		allNodes = append(allNodes, taskAllNodes)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server network tasks: %w", err)
	}
	for index := range tasks {
		tasks[index].Targets, err = s.listServerProbeTargets(ctx, tasks[index].ID, allNodes[index])
		if err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

func (s *NetworkTaskStore) listServerProbeTargets(
	ctx context.Context,
	taskID string,
	allNodes bool,
) ([]model.NetworkProbeTarget, error) {
	query := `SELECT n.id, n.name, n.alias, n.host, n.port FROM nodes n`
	args := []any{}
	if allNodes {
		query += ` WHERE n.credential_state <> 'pending_claim'`
	} else {
		query += ` JOIN network_task_nodes a ON a.node_id = n.id WHERE a.task_id = ? AND ` + hiddenNodeFilter
		args = append(args, taskID)
	}
	query += ` ORDER BY n.sort_order ASC, n.name ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query server probe targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	targets := []model.NetworkProbeTarget{}
	for rows.Next() {
		var target model.NetworkProbeTarget
		if err := rows.Scan(&target.NodeID, &target.Name, &target.Alias, &target.Host, &target.Port); err != nil {
			return nil, fmt.Errorf("scan server probe target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server probe targets: %w", err)
	}
	return targets, nil
}
