package theme

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

const themeOwnershipMarker = ".beat-package"

func (service *Service) reconcilePackages(ctx context.Context) error {
	deleted, err := service.store.ListDeletedPackages(ctx)
	if err != nil {
		return err
	}
	for index := range deleted {
		// 逻辑删除已完成；失败留待下一次 DELETE 或启动重试。
		_ = service.finishDeletion(ctx, &deleted[index])
	}
	root, err := os.OpenRoot(service.root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := service.reconcileDirectory(ctx, root, "staging"); err != nil {
		return err
	}
	return service.reconcileDirectory(ctx, root, "packages")
}

func (service *Service) reconcileDirectory(ctx context.Context, root *os.Root, directory string) error {
	if err := rejectAssetLinks(root, directory); err != nil {
		return err
	}
	entries, err := readThemeDirectory(root, directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := path.Join(directory, entry.Name())
		digest, err := ownedThemeDigest(root, name)
		if err != nil {
			continue
		}
		if directory == "staging" && strings.HasPrefix(entry.Name(), "theme-") {
			if err := root.RemoveAll(name); err != nil {
				return err
			}
			continue
		}
		if directory == "packages" && digest == entry.Name() {
			if err := service.reconcileOrphan(ctx, digest); err != nil {
				return err
			}
		}
	}
	return nil
}

func (service *Service) reconcileOrphan(ctx context.Context, digest string) error {
	pkg, err := service.store.GetPackageByDigest(ctx, digest)
	if err != nil || pkg != nil {
		return err
	}
	return service.quarantinePackage(digest)
}

func readThemeDirectory(root *os.Root, name string) ([]os.DirEntry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return file.ReadDir(-1)
}

func ownedThemeDigest(root *os.Root, name string) (string, error) {
	marker := path.Join(name, themeOwnershipMarker)
	if err := rejectAssetLinks(root, marker); err != nil {
		return "", err
	}
	file, err := root.Open(marker)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != 64 {
		return "", ErrContentUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, 65))
	if err != nil {
		return "", err
	}
	if !validDigest(string(raw)) {
		return "", ErrContentUnavailable
	}
	return string(raw), nil
}

func removeOwnedThemeDirectory(root *os.Root, name, digest string) error {
	if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	stored, err := ownedThemeDigest(root, name)
	if err != nil {
		return err
	}
	if stored != digest {
		return ErrContentUnavailable
	}
	return root.RemoveAll(name)
}

func (service *Service) clearDeletedContent(digest string) error {
	root, err := os.OpenRoot(service.root)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return removeOwnedThemeDirectory(root, path.Join("packages", digest), digest)
}
