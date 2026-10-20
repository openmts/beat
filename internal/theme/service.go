package theme

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// Service 管理主题包安装、激活、回滚、导出与删除。
type Service struct {
	store       *store.ThemeStore
	users       *store.AdminStore
	root        string // <data-dir>/themes
	beatVersion [3]int
	now         func() time.Time
	mutations   operationGate
}

// NewService 构造主题服务；root 为可写的主题数据目录，beatVersion 为当前版本。
func NewService(
	store *store.ThemeStore, users *store.AdminStore, root, beatVersion string, now func() time.Time,
) (*Service, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create theme directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure theme directory: %w", err)
	}
	for _, sub := range []string{"packages", "staging", "quarantine"} {
		path := filepath.Join(root, sub)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("create theme %s directory: %w", sub, err)
		}
	}
	service := &Service{store: store, users: users, root: root,
		beatVersion: parseVersion(beatVersion), now: now}
	if err := service.reconcilePackages(context.Background()); err != nil {
		return nil, fmt.Errorf("reconcile theme packages: %w", err)
	}
	return service, nil
}

func currentPackage(activation *model.ThemeActivation) string {
	if activation == nil {
		return ""
	}
	return activation.PackageID
}

// MarkUnhealthy 标记当前激活不健康（运行时失败后调用）。
func (service *Service) MarkUnhealthy(ctx context.Context, category string) error {
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return err
	}
	defer release()
	return service.store.MarkActivationUnhealthy(ctx, category, service.now())
}

// ActiveDescriptor 返回公开的安全活动主题描述。
func (service *Service) ActiveDescriptor(ctx context.Context) (model.ActiveThemeDescriptor, error) {
	builtIn := model.ActiveThemeDescriptor{
		ThemeID: "built-in", Name: "Built-in", Version: "1.0.0",
		Runtime: model.ThemeRuntimeDeclarative, BuiltIn: true, Protocol: 2,
	}
	activation, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	if activation == nil || activation.PackageID == "" {
		return builtIn, nil
	}
	pkg, err := service.store.GetPackageByID(ctx, activation.PackageID)
	if err != nil {
		return model.ActiveThemeDescriptor{}, err
	}
	if pkg == nil || pkg.State != model.ThemeStateInstalled {
		return builtIn, nil
	}
	descriptor, err := service.describeStoredPackage(ctx, pkg)
	if err != nil {
		return builtIn, nil
	}
	return descriptor, nil
}

// sandboxEntryFor 从清单中解析沙箱入口 HTML 文件名。
func sandboxEntryFor(pkg *model.ThemePackage) string {
	var manifest model.ThemeManifest
	if err := json.Unmarshal([]byte(pkg.ManifestJSON), &manifest); err != nil {
		return ""
	}
	if pkg.Runtime != model.ThemeRuntimeSandboxed {
		return ""
	}
	entry, err := sandboxEntry(manifest)
	if err != nil {
		return ""
	}
	return entry
}

// ListPackages 列出包（附活动/健康标记）。
func (service *Service) ListPackages(ctx context.Context) ([]model.ThemePackage, error) {
	packages, err := service.store.ListPackages(ctx)
	if err != nil {
		return nil, err
	}
	activation, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return nil, err
	}
	for index := range packages {
		if manifest, err := parseManifest([]byte(packages[index].ManifestJSON)); err == nil {
			packages[index].PreviewURL = themeAssetPath(packages[index].ArchiveDigest, manifest.Preview)
		}
		if activation != nil && activation.PackageID == packages[index].ID {
			packages[index].IsActive = true
			packages[index].HealthState = activation.HealthState
		}
	}
	return packages, nil
}

func (service *Service) validateCompatibility(manifest model.ThemeManifest) error {
	minimum := parseVersion(manifest.MinimumBeatVersion)
	if manifest.MaximumBeatVersion != "" && compareVersions(service.beatVersion, parseVersion(manifest.MaximumBeatVersion)) > 0 {
		return ErrRuntimeIncompatible
	}
	if compareVersions(service.beatVersion, minimum) < 0 {
		return ErrRuntimeIncompatible
	}
	return nil
}
