package theme

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// PackageDeletion 明确区分已完成的逻辑删除和可重试的文件清理。
type PackageDeletion struct {
	PackageID      string `json:"package_id"`
	State          string `json:"state"`
	CleanupPending bool   `json:"cleanup_pending"`
}

func (service *Service) DeletePackage(ctx context.Context, id, actorID string) (PackageDeletion, error) {
	release, err := service.mutations.enter(ctx)
	if err != nil {
		return PackageDeletion{}, err
	}
	defer release()
	pkg, err := service.store.DeletePackage(ctx, id, service.now())
	if errors.Is(err, store.ErrThemePackageNotFound) {
		return PackageDeletion{}, ErrContentUnavailable
	}
	if errors.Is(err, store.ErrThemePackageActive) {
		return PackageDeletion{}, ErrActivePackage
	}
	if err != nil {
		return PackageDeletion{}, err
	}
	result := PackageDeletion{PackageID: id, State: model.ThemeStateDeleted}
	result.CleanupPending = service.finishDeletion(ctx, pkg) != nil
	return result, nil
}

// Delete 保留服务内只需逻辑删除的调用方式；HTTP 使用 DeletePackage 返回清理状态。
func (service *Service) Delete(ctx context.Context, id, actorID string) error {
	_, err := service.DeletePackage(ctx, id, actorID)
	return err
}

func (service *Service) finishDeletion(ctx context.Context, pkg *model.ThemePackage) error {
	if err := service.quarantinePackage(pkg.ArchiveDigest); err != nil {
		return err
	}
	return service.store.CompletePackageDeletion(ctx, pkg.ID, service.now())
}

func (service *Service) quarantinePackage(digest string) error {
	if !validDigest(digest) {
		return ErrContentUnavailable
	}
	root, err := os.OpenRoot(service.root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	source, target := path.Join("packages", digest), path.Join("quarantine", digest)
	if err := rejectAssetLinks(root, source); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syncPackageParents(root)
		}
		return err
	}
	if err := rejectAssetLinks(root, "quarantine"); err != nil {
		return err
	}
	if err := removeOwnedThemeDirectory(root, target, digest); err != nil {
		return err
	}
	if err := root.Rename(source, target); err != nil {
		return err
	}
	return syncPackageParents(root)
}

// Export 只返回仍安装且摘要匹配的归档，不读取损坏数据库指定的任意路径。
func (service *Service) Export(ctx context.Context, id string) ([]byte, string, error) {
	pkg, err := service.store.GetPackageByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if pkg == nil || pkg.State != model.ThemeStateInstalled || !validDigest(pkg.ArchiveDigest) ||
		pkg.ArchiveBytes <= 0 || pkg.ArchiveBytes > maxPackageBytes {
		return nil, "", ErrContentUnavailable
	}
	raw, err := service.readPackageArchive(pkg)
	if err != nil {
		return nil, "", ErrContentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return raw, fmt.Sprintf("%s-%s.theme.zip", pkg.ThemeID, pkg.Version), nil
}

func (service *Service) readPackageArchive(pkg *model.ThemePackage) ([]byte, error) {
	root, err := os.OpenRoot(service.root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	name := path.Join("packages", pkg.ArchiveDigest, "archive.zip")
	if err := rejectAssetLinks(root, name); err != nil {
		return nil, err
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != pkg.ArchiveBytes {
		return nil, ErrContentUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, pkg.ArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != pkg.ArchiveBytes || sha256Hex(raw) != pkg.ArchiveDigest {
		return nil, ErrContentUnavailable
	}
	return raw, nil
}
