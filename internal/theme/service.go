package theme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

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
	return &Service{store: store, users: users, root: root,
		beatVersion: parseVersion(beatVersion), now: now}, nil
}

// Install 校验并安装一个主题包（不激活）。
func (service *Service) Install(
	ctx context.Context, raw []byte, sourceType, sourceID, actorID string,
) (*model.ThemePackage, error) {
	validated, err := ValidateArchive(raw)
	if err != nil {
		return nil, err
	}
	existing, err := service.store.GetPackageByDigest(ctx, validated.Digest)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrPackageConflict
	}
	if err := service.validateCompatibility(validated.Manifest); err != nil {
		return nil, err
	}
	packageID := uuid.NewString()
	staging, err := os.MkdirTemp(filepath.Join(service.root, "staging"), "theme-")
	if err != nil {
		return nil, fmt.Errorf("create theme staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := os.Chmod(staging, 0o700); err != nil {
		return nil, fmt.Errorf("secure theme staging directory: %w", err)
	}
	if err := service.stageArchive(staging, validated, raw); err != nil {
		return nil, err
	}

	packageDir := filepath.Join(service.root, "packages", validated.Digest)
	if err := os.Rename(staging, packageDir); err != nil {
		return nil, fmt.Errorf("commit theme package: %w", err)
	}
	now := service.now()
	manifestJSON, err := json.Marshal(validated.Manifest)
	if err != nil {
		return nil, fmt.Errorf("encode theme manifest: %w", err)
	}
	pkg := &model.ThemePackage{
		ID: packageID, ThemeID: validated.Manifest.ID, Name: validated.Manifest.Name,
		Version: validated.Manifest.Version, Author: validated.Manifest.Author,
		Description: validated.Manifest.Description, License: validated.Manifest.License,
		Runtime: validated.Manifest.Runtime, ManifestSchema: validated.Manifest.SchemaVersion,
		MinimumBeatVersion: validated.Manifest.MinimumBeatVersion,
		MaximumBeatVersion: validated.Manifest.MaximumBeatVersion,
		ArchiveDigest:      validated.Digest, ArchiveBytes: validated.ArchiveBytes,
		ContentBytes: validated.ContentBytes, ManifestJSON: string(manifestJSON),
		ConfigurationJSON: "{}", SourceType: sourceType, SourceID: sourceID,
		State: model.ThemeStateInstalled, ValidatedAt: now, CreatorID: actorID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := service.store.CreatePackage(ctx, pkg); err != nil {
		_ = os.RemoveAll(packageDir)
		return nil, err
	}
	return pkg, nil
}

// stageArchive 将校验后的归档写入暂存目录（archive.zip + content/*）。
func (service *Service) stageArchive(staging string, validated *ValidatedPackage, raw []byte) error {
	if err := writePrivate(filepath.Join(staging, "archive.zip"), raw); err != nil {
		return err
	}
	contentDir := filepath.Join(staging, "content")
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		return fmt.Errorf("create theme content directory: %w", err)
	}
	for _, file := range validated.Files {
		content := validated.Content[file.Path]
		destination := filepath.Join(contentDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create theme content parent: %w", err)
		}
		if err := writePrivate(destination, content); err != nil {
			return err
		}
	}
	return nil
}

// writePrivate 以 0600 权限写入文件。
func writePrivate(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create theme file: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write theme file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync theme file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close theme file: %w", err)
	}
	return nil
}

// Activate 激活指定包；兼容性已校验。
func (service *Service) Activate(ctx context.Context, packageID, actorID string) (*model.ThemePackage, error) {
	pkg, err := service.store.GetPackageByID(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg == nil || pkg.State != model.ThemeStateInstalled {
		return nil, ErrContentUnavailable
	}
	current, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return nil, err
	}
	previous := ""
	if current != nil {
		previous = current.PackageID
	}
	now := service.now()
	if err := service.store.ActivatePackage(ctx, &model.ThemeActivation{
		ID: uuid.NewString(), PackageID: packageID, PreviousPackageID: previous,
		ActorID: actorID, Reason: model.ThemeActivationReason, ActivatedAt: now,
	}, now); err != nil {
		return nil, err
	}
	return pkg, nil
}

// Rollback 回滚到最近的健康包或内置主题。
func (service *Service) Rollback(ctx context.Context, actorID string) (string, error) {
	current, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return "", err
	}
	target := ""
	if current != nil && current.PreviousPackageID != "" {
		target = current.PreviousPackageID
	}
	now := service.now()
	if err := service.store.ActivatePackage(ctx, &model.ThemeActivation{
		ID: uuid.NewString(), PackageID: target, PreviousPackageID: currentPackage(current),
		ActorID: actorID, Reason: model.ThemeRollbackReason, ActivatedAt: now,
	}, now); err != nil {
		return "", err
	}
	if target == "" {
		return "built-in", nil
	}
	return target, nil
}

func currentPackage(activation *model.ThemeActivation) string {
	if activation == nil {
		return ""
	}
	return activation.PackageID
}

// Fallback 立即回退到内置主题。
func (service *Service) Fallback(ctx context.Context, actorID, category string) error {
	current, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return err
	}
	now := service.now()
	if err := service.store.ActivatePackage(ctx, &model.ThemeActivation{
		ID: uuid.NewString(), PreviousPackageID: currentPackage(current),
		ActorID: actorID, Reason: model.ThemeFallbackReason, ActivatedAt: now,
	}, now); err != nil {
		return err
	}
	return nil
}

// MarkUnhealthy 标记当前激活不健康（运行时失败后调用）。
func (service *Service) MarkUnhealthy(ctx context.Context, category string) error {
	return service.store.MarkActivationUnhealthy(ctx, category, service.now())
}

// Export 返回精确的已校验归档与安全文件名。
func (service *Service) Export(ctx context.Context, packageID string) ([]byte, string, error) {
	pkg, err := service.store.GetPackageByID(ctx, packageID)
	if err != nil {
		return nil, "", err
	}
	if pkg == nil || pkg.State == model.ThemeStateDeleted {
		return nil, "", ErrContentUnavailable
	}
	path := filepath.Join(service.root, "packages", pkg.ArchiveDigest, "archive.zip")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ErrContentUnavailable
	}
	filename := fmt.Sprintf("%s-%s.theme.zip", pkg.ThemeID, pkg.Version)
	return raw, filename, nil
}

// Delete 隔离并删除包（活动包不可删除）。
func (service *Service) Delete(ctx context.Context, packageID, actorID string) error {
	pkg, err := service.store.GetPackageByID(ctx, packageID)
	if err != nil {
		return err
	}
	if pkg == nil || pkg.State == model.ThemeStateDeleted {
		return ErrContentUnavailable
	}
	current, err := service.store.CurrentActivation(ctx)
	if err != nil {
		return err
	}
	if current != nil && current.PackageID == packageID {
		return ErrActivePackage
	}
	quarantine := filepath.Join(service.root, "quarantine", pkg.ArchiveDigest)
	if err := os.Rename(filepath.Join(service.root, "packages", pkg.ArchiveDigest), quarantine); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("quarantine theme package: %w", err)
	}
	if err := service.store.UpdatePackageState(ctx, packageID, model.ThemeStateQuarantined, "deleted", service.now()); err != nil {
		return err
	}
	_ = actorID
	return nil
}

// ActiveDescriptor 返回公开的安全活动主题描述。
func (service *Service) ActiveDescriptor(ctx context.Context) (model.ActiveThemeDescriptor, error) {
	builtIn := model.ActiveThemeDescriptor{
		ThemeID: "built-in", Name: "Built-in", Version: "1.0.0",
		Runtime: model.ThemeRuntimeDeclarative, BuiltIn: true, Protocol: 1,
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
	return model.ActiveThemeDescriptor{
		PackageID: pkg.ID, ThemeID: pkg.ThemeID, Name: pkg.Name, Version: pkg.Version,
		Runtime: pkg.Runtime, Protocol: 1, Configuration: pkg.ConfigurationJSON,
		SandboxEntry: sandboxEntryFor(pkg), ArchiveDigest: pkg.ArchiveDigest,
	}, nil
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
	for _, file := range manifest.Files {
		if strings.HasSuffix(file.Path, ".html") {
			return file.Path
		}
	}
	return ""
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
		if activation != nil && activation.PackageID == packages[index].ID {
			packages[index].IsActive = true
			packages[index].HealthState = activation.HealthState
		}
	}
	return packages, nil
}

// Content 打开活动包内容目录（供资产服务使用）。
func (service *Service) Content(packageID string) (string, error) {
	if packageID == "" {
		return "", ErrContentUnavailable
	}
	return filepath.Join(service.root, "packages", packageID, "content"), nil
}

// PackageDir 返回包目录。
func (service *Service) PackageDir(packageID string) string {
	return filepath.Join(service.root, "packages", packageID)
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

func parseVersion(version string) [3]int {
	core := version
	if index := strings.IndexAny(core, "+-"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	var result [3]int
	for index := 0; index < len(parts) && index < 3; index++ {
		for _, character := range parts[index] {
			if character < '0' || character > '9' {
				break
			}
			result[index] = result[index]*10 + int(character-'0')
		}
	}
	return result
}

func compareVersions(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
