package theme

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

// Asset 只包含已安装清单允许且摘要匹配的公开资源。
type Asset struct {
	Content []byte
	MIME    string
	SHA256  string
}

func (service *Service) ReadAsset(ctx context.Context, digest, name string) (Asset, error) {
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	file, err := service.assetDeclaration(ctx, digest, name)
	if err != nil {
		return Asset{}, err
	}
	content, err := service.readAssetFile(digest, file)
	if err != nil {
		return Asset{}, ErrContentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	mime, err := validateAssetContent(name, content)
	if err != nil {
		return Asset{}, ErrContentUnavailable
	}
	return Asset{Content: content, MIME: mime, SHA256: file.SHA256}, nil
}

func (service *Service) assetDeclaration(ctx context.Context, digest, name string) (model.ThemeManifestFile, error) {
	if !validDigest(digest) {
		return model.ThemeManifestFile{}, ErrContentUnavailable
	}
	if _, err := validateEntryPath(name); err != nil {
		return model.ThemeManifestFile{}, ErrContentUnavailable
	}
	pkg, err := service.store.GetPackageByDigest(ctx, digest)
	if err != nil {
		return model.ThemeManifestFile{}, err
	}
	if pkg == nil || pkg.State != model.ThemeStateInstalled {
		return model.ThemeManifestFile{}, ErrContentUnavailable
	}
	manifest, err := parseManifest([]byte(pkg.ManifestJSON))
	if err != nil {
		return model.ThemeManifestFile{}, ErrContentUnavailable
	}
	file, ok := manifestAsset(manifest, name)
	if !ok {
		return model.ThemeManifestFile{}, ErrContentUnavailable
	}
	return file, nil
}

func manifestAsset(manifest model.ThemeManifest, name string) (model.ThemeManifestFile, bool) {
	for _, file := range manifest.Files {
		if file.Path == name && file.Size >= 0 && file.Size <= maxAssetFileBytes && validDigest(file.SHA256) {
			return file, true
		}
	}
	return model.ThemeManifestFile{}, false
}

func (service *Service) readAssetFile(digest string, declared model.ThemeManifestFile) ([]byte, error) {
	root, err := os.OpenRoot(service.root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	name := path.Join("packages", digest, "content", declared.Path)
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
	if !info.Mode().IsRegular() || info.Size() != declared.Size {
		return nil, ErrContentUnavailable
	}
	content, err := io.ReadAll(io.LimitReader(file, declared.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != declared.Size || sha256Hex(content) != declared.SHA256 {
		return nil, ErrContentUnavailable
	}
	return content, nil
}

func rejectAssetLinks(root *os.Root, name string) error {
	parts := strings.Split(name, "/")
	for index := range parts {
		info, err := root.Lstat(path.Join(parts[:index+1]...))
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrContentUnavailable
		}
		if index < len(parts)-1 && !info.IsDir() {
			return ErrContentUnavailable
		}
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		digit := character >= '0' && character <= '9'
		hexadecimal := character >= 'a' && character <= 'f'
		if !digit && !hexadecimal {
			return false
		}
	}
	return true
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
