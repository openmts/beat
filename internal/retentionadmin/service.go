// Package retentionadmin 把「发布对象保留期」从启动环境变量扩展到后台可配置：
// 管理员显式保存的配置优先，没保存过时回落到环境变量缺省（升级后行为不变）。
package retentionadmin

import (
	"context"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/model"
)

// ErrDisabled 表示当前既没有环境变量缺省也没有后台配置，自动清理没有生效。
var ErrDisabled = errors.New("agent release retention is disabled")

// SettingsStore 是保留期设置的持久化端口；实现见 internal/store。
type SettingsStore interface {
	Get(context.Context) (model.AgentRetentionSettings, error)
	Update(context.Context, model.AgentRetentionSettings) error
	Reset(context.Context) error
	MarkRun(context.Context, string, string, string, model.AgentRetentionRunStatus) error
}

// Pruner 是自动清理的执行者，由 agentrollout.Service 实现。
type Pruner interface {
	Prune(context.Context, agentrollout.RetentionPolicy) (agentrollout.RetentionCounts, error)
}

// Service 串起设置、有效策略与一次清理的执行与记账。
type Service struct {
	store       SettingsStore
	pruner      Pruner
	environment agentrollout.RetentionPolicy
}

func New(store SettingsStore, pruner Pruner, environment agentrollout.RetentionPolicy) *Service {
	return &Service{store: store, pruner: pruner, environment: environment}
}

// Get 返回后台视图：显式配置、环境变量缺省与最近一轮结果。
func (service *Service) Get(ctx context.Context) (model.AgentRetentionSettings, error) {
	if service == nil || service.store == nil {
		return model.AgentRetentionSettings{}, fmt.Errorf("agent release retention is unavailable")
	}
	settings, err := service.store.Get(ctx)
	if err != nil {
		return model.AgentRetentionSettings{}, err
	}
	settings.Environment = service.defaults()
	return settings, nil
}

// Update 保存管理员配置；非法保留期由 model 校验拒绝。
func (service *Service) Update(ctx context.Context, input model.AgentRetentionSettings) (model.AgentRetentionSettings, error) {
	if service == nil || service.store == nil {
		return model.AgentRetentionSettings{}, fmt.Errorf("agent release retention is unavailable")
	}
	if err := input.Validate(); err != nil {
		return model.AgentRetentionSettings{}, err
	}
	if err := service.store.Update(ctx, input); err != nil {
		return model.AgentRetentionSettings{}, err
	}
	return service.Get(ctx)
}

// Reset 丢弃后台配置，回到启动环境变量缺省。
func (service *Service) Reset(ctx context.Context) (model.AgentRetentionSettings, error) {
	if service == nil || service.store == nil {
		return model.AgentRetentionSettings{}, fmt.Errorf("agent release retention is unavailable")
	}
	if err := service.store.Reset(ctx); err != nil {
		return model.AgentRetentionSettings{}, err
	}
	return service.Get(ctx)
}

// Run 立即执行一轮清理（管理员手动触发），并把结果写回设置行。
func (service *Service) Run(ctx context.Context) (model.AgentRetentionSettings, error) {
	if service == nil || service.store == nil || service.pruner == nil {
		return model.AgentRetentionSettings{}, fmt.Errorf("agent release retention is unavailable")
	}
	policy, err := service.Effective(ctx)
	if err != nil {
		return model.AgentRetentionSettings{}, err
	}
	if !policy.Enabled() {
		return model.AgentRetentionSettings{}, ErrDisabled
	}
	return service.prune(ctx, policy, model.AgentRetentionTriggerManual)
}

// RunScheduled 执行一轮调度触发的清理：没有生效保留期时保持空操作，也不写状态行。
func (service *Service) RunScheduled(ctx context.Context) error {
	policy, err := service.Effective(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled() {
		return nil
	}
	_, err = service.prune(ctx, policy, model.AgentRetentionTriggerSchedule)
	return err
}

// Effective 返回当前生效的保留期：后台显式配置优先，否则用环境变量缺省。
func (service *Service) Effective(ctx context.Context) (agentrollout.RetentionPolicy, error) {
	if service == nil || service.store == nil {
		return agentrollout.RetentionPolicy{}, fmt.Errorf("agent release retention is unavailable")
	}
	settings, err := service.store.Get(ctx)
	if err != nil {
		return agentrollout.RetentionPolicy{}, err
	}
	if settings.Configured {
		if err := settings.Validate(); err != nil {
			return agentrollout.RetentionPolicy{}, err
		}
		return agentrollout.RetentionPolicy{RolloutDays: settings.RolloutDays, ReleaseDays: settings.ReleaseDays}, nil
	}
	return service.environment, nil
}

// prune 执行一轮清理并记录结果；清理失败也写回原因，后台能看到最近一次为什么没删掉东西。
func (service *Service) prune(ctx context.Context, policy agentrollout.RetentionPolicy, trigger string) (model.AgentRetentionSettings, error) {
	counts, err := service.pruner.Prune(ctx, policy)
	status, reason := model.AgentRetentionStatusOK, ""
	if err != nil {
		status, reason = model.AgentRetentionStatusFailed, err.Error()
	}
	record := model.AgentRetentionRunStatus{LastRollouts: counts.Rollouts, LastReleases: counts.Releases, LastArtifacts: counts.Artifacts}
	if markErr := service.store.MarkRun(ctx, status, reason, trigger, record); markErr != nil {
		return model.AgentRetentionSettings{}, errors.Join(err, markErr)
	}
	settings, getErr := service.Get(ctx)
	if getErr != nil {
		return model.AgentRetentionSettings{}, errors.Join(err, getErr)
	}
	return settings, err
}

func (service *Service) defaults() model.AgentRetentionDefaults {
	return model.AgentRetentionDefaults{RolloutDays: service.environment.RolloutDays,
		ReleaseDays: service.environment.ReleaseDays, Enabled: service.environment.Enabled()}
}
