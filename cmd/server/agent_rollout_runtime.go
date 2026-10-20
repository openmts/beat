package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/retentionadmin"
)

const agentRolloutRecoveryInterval = 5 * time.Second

// agentRolloutRetentionInterval 控制保留期自动清理的检查频率；清理按批进行，空闲轮次不写库。
const agentRolloutRetentionInterval = 6 * time.Hour

// agentRolloutRetentionDeadline 限制单轮清理的最长时间，避免长事务拖住写入。
const agentRolloutRetentionDeadline = 15 * time.Minute

type rolloutMonitor interface {
	Enabled() bool
	Ready(context.Context) error
}

func rolloutMonitoring(services *serverServices) rolloutMonitor {
	if services.agentRollouts == nil {
		return nil
	}
	return services.agentRollouts
}

func (operations *operability) agentRolloutCheck(ctx context.Context) readinessCheck {
	if operations.agentRollouts == nil || !operations.agentRollouts.Enabled() {
		return readinessCheck{Status: "ok", Detail: "disabled"}
	}
	if err := operations.agentRollouts.Ready(ctx); err != nil {
		return readinessCheck{Status: "error", Detail: "unavailable"}
	}
	return readinessCheck{Status: "ok"}
}

func startAgentRolloutWorker(config backgroundConfig, tasks *backgroundTasks) {
	service := config.services.agentRollouts
	if service == nil || !service.Enabled() {
		return
	}
	worker := config.runtime.workers.register("agent_rollouts", agentRolloutRecoveryInterval)
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{interval: agentRolloutRecoveryInterval, run: service.Recover, observe: worker.observe()})
	})
	startAgentRolloutRetentionWorker(config, tasks, service)
}

// startAgentRolloutRetentionWorker 按保留期清理终态 rollout 与已撤销的发布；未配置保留期时
// 不注册 worker，保持默认的「保留全部历史」行为。
func startAgentRolloutRetentionWorker(config backgroundConfig, tasks *backgroundTasks, service *agentrollout.Service) {
	admin := config.services.agentRetentionAdmin
	// 环境变量缺省与后台配置任一开启就注册；真的要清理什么由每一轮的有效策略决定，
	// 这样后台刚打开开关不必重启进程。
	if !config.services.agentRetention.Enabled() && !agentRetentionConfigured(config.ctx, admin) {
		return
	}
	worker := config.runtime.workers.register("agent_rollout_retention", agentRolloutRetentionInterval)
	config.runtime.workers.start(&tasks.wait, worker.in(config.ctx), func(ctx context.Context) {
		runCommercialPeriodic(ctx, commercialPeriod{
			interval: agentRolloutRetentionInterval,
			run: func(ctx context.Context) error {
				return pruneAgentRollouts(ctx, admin, service)
			},
			observe: worker.observe(),
		})
	})
}

// agentRetentionConfigured 报告后台是否已有显式配置；存储不可用时按「没有配置」处理，
// 启动阶段不因为读设置失败而拒绝服务。
func agentRetentionConfigured(ctx context.Context, admin *retentionadmin.Service) bool {
	if admin == nil {
		return false
	}
	settings, err := admin.Get(ctx)
	if err != nil {
		slog.WarnContext(ctx, "agent release retention settings are unavailable", "error", err)
		return false
	}
	return settings.Configured && settings.Enabled()
}

// pruneAgentRollouts 执行一轮有截止时间的清理；只在真的删掉对象时记录日志。
// 保留期每一轮都从设置行重新读取，后台改动无需重启进程。
func pruneAgentRollouts(ctx context.Context, admin *retentionadmin.Service, service *agentrollout.Service) error {
	if admin == nil {
		return nil
	}
	runCtx, cancel := context.WithTimeout(ctx, agentRolloutRetentionDeadline)
	err := admin.RunScheduled(runCtx)
	cancel()
	if err != nil {
		return err
	}
	settings, getErr := admin.Get(ctx)
	if getErr != nil {
		return getErr
	}
	counts := agentrollout.RetentionCounts{Rollouts: settings.Status.LastRollouts,
		Releases: settings.Status.LastReleases, Artifacts: settings.Status.LastArtifacts}
	if counts.Rollouts > 0 || counts.Releases > 0 {
		slog.InfoContext(ctx, "agent rollout retention pruned objects", "rollouts", counts.Rollouts, "releases", counts.Releases, "artifacts", counts.Artifacts)
	}
	return nil
}

func writeAgentRolloutMetrics(output *strings.Builder, check readinessCheck) {
	enabled := check.Detail != "disabled"
	_, _ = fmt.Fprintf(output, "# TYPE beat_agent_rollout_enabled gauge\nbeat_agent_rollout_enabled %d\n", boolMetric(enabled))
	_, _ = fmt.Fprintf(output, "# TYPE beat_agent_rollout_ready gauge\nbeat_agent_rollout_ready %d\n", boolMetric(enabled && check.Status == "ok"))
}
