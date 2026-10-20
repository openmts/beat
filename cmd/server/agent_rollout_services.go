package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrolloutremote"
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/releasesignature"
	"github.com/beat/backend/internal/retentionadmin"
	"github.com/beat/backend/internal/store"
)

// 发布对象保留期开关：缺省 0，表示保留全部历史；只有显式给出正整数才启动自动清理。
const (
	agentRolloutRetentionEnv = "BEAT_AGENT_ROLLOUT_RETENTION_DAYS"
	agentReleaseRetentionEnv = "BEAT_AGENT_RELEASE_RETENTION_DAYS"
)

func (services *serverServices) initializeAgentRollouts(ctx context.Context, stores *serverStores, dataDir string) error {
	config, err := readRolloutConfiguration(os.Getenv("BEAT_AGENT_ROLLOUT_CONFIG"))
	if err != nil {
		return err
	}
	options := agentrollout.Config{Database: stores.sqlite.DB, Enabled: config.Enabled}
	if err := stores.sqlite.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&options.ServerSchema); err != nil {
		return fmt.Errorf("read Agent rollout server schema: %w", err)
	}
	if config.Enabled {
		if err := configureRolloutTrust(&options, config, dataDir); err != nil {
			return err
		}
	}
	service, err := agentrollout.NewContext(ctx, options)
	if err != nil {
		return fmt.Errorf("initialize Agent rollout service: %w", err)
	}
	var releaseRemote *agentrolloutremote.Service
	if config.Enabled {
		releaseRemote, err = newAgentReleaseRemote(service, os.Getenv)
		if err != nil {
			return fmt.Errorf("initialize Agent release source: %w", err)
		}
	}
	if err := service.Recover(ctx); err != nil {
		return fmt.Errorf("recover Agent rollouts: %w", err)
	}
	policy, err := readAgentRetentionPolicy(os.Getenv)
	if err != nil {
		return err
	}
	services.agentRollouts = service
	services.agentReleaseRemote = releaseRemote
	services.agentRetention = policy
	// 后台可配置的保留期：显式配置优先，没有配置时回落到上面解析出的环境变量缺省。
	services.agentRetentionAdmin = retentionadmin.New(store.NewAgentRetentionStore(stores.sqlite.DB), service, policy)
	return nil
}

// agentRetentionRouterOption 只在真的建出保留期服务时注入，避免把 typed-nil 交给路由层。
func agentRetentionRouterOption(services *serverServices) api.RouterOption {
	if services == nil || services.agentRetentionAdmin == nil {
		return func(*api.Router) {}
	}
	return api.WithAgentRetentionService(services.agentRetentionAdmin)
}

// readAgentRetentionPolicy 解析发布对象保留期；非法或越界的值让启动失败，而不是静默降级。
func readAgentRetentionPolicy(environment func(string) string) (agentrollout.RetentionPolicy, error) {
	rollouts, err := retentionDays(environment(agentRolloutRetentionEnv))
	if err != nil {
		return agentrollout.RetentionPolicy{}, err
	}
	releases, err := retentionDays(environment(agentReleaseRetentionEnv))
	if err != nil {
		return agentrollout.RetentionPolicy{}, err
	}
	policy := agentrollout.RetentionPolicy{RolloutDays: rollouts, ReleaseDays: releases}
	if !policy.Valid() {
		return agentrollout.RetentionPolicy{}, errRolloutConfiguration
	}
	return policy, nil
}

func retentionDays(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(value)
	if err != nil {
		return 0, errRolloutConfiguration
	}
	return days, nil
}

func configureRolloutTrust(options *agentrollout.Config, config rolloutConfiguration, dataDir string) error {
	directory, err := filepath.Abs(dataDir)
	if err != nil {
		return errRolloutConfiguration
	}
	if config.SignatureMode == rolloutSignatureModePinned {
		options.Keys, options.PinnedOnly = config.trustedKeys(), true
		options.Directory = filepath.Join(directory, "agent-release-cache")
		return nil
	}
	root, err := releasesignature.ReadTrustFile(config.Sigstore.TrustedRootFile)
	if err != nil {
		return errRolloutConfiguration
	}
	verifier, err := releasesignature.New(releasesignature.Config{
		Executable: config.Sigstore.Executable, TrustedRoot: root, TemporaryDirectory: directory,
		CertificateIdentity: config.Sigstore.CertificateIdentity, ReleaseWorkflow: config.Sigstore.ReleaseWorkflow,
		OIDCIssuer: config.Sigstore.OIDCIssuer,
	})
	if err != nil {
		return errRolloutConfiguration
	}
	options.Keys, options.Sigstore = config.trustedKeys(), verifier
	options.Directory = filepath.Join(directory, "agent-release-cache")
	return nil
}

func agentRolloutRouterOption(services *serverServices) api.RouterOption {
	return func(router *api.Router) {
		api.WithAgentRollouts(services.agentRollouts)(router)
		api.WithAgentReleaseRemote(services.agentReleaseRemote)(router)
	}
}
