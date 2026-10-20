package theme

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// PackOptions 允许输出位于源码目录，但输出本身永远不会进入归档。
type PackOptions struct {
	Source string
	Output string
}

func BuildArchive(options PackOptions) ([]byte, error) {
	root, err := os.OpenRoot(options.Source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	metadata, err := readPackFile(root, manifestName, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, err := parseManifest(metadata)
	if err != nil {
		return nil, err
	}
	files, err := sourceFiles(root, options)
	if err != nil {
		return nil, err
	}
	manifest.Files = nil
	for _, name := range sortedFileNames(files) {
		content := files[name]
		manifest.Files = append(manifest.Files, manifestFile(name, content))
	}
	metadata, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	files[manifestName] = append(metadata, '\n')
	raw, err := zipFiles(files)
	if err != nil {
		return nil, err
	}
	if _, err := ValidateArchive(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func sourceFiles(root *os.Root, options PackOptions) (map[string][]byte, error) {
	files := make(map[string][]byte)
	source, err := filepath.Abs(options.Source)
	if err != nil {
		return nil, err
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return nil, err
	}
	if output == filepath.Join(source, manifestName) {
		return nil, ErrArchiveInvalid
	}
	var total int
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." || name == manifestName || filepath.Join(source, filepath.FromSlash(name)) == output {
			return nil
		}
		if _, err := validateEntryPath(name); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrArchiveInvalid
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() || len(files)+1 >= maxFiles {
			return ErrArchiveInvalid
		}
		content, err := readPackFile(root, name, maxAssetFileBytes)
		if err != nil {
			return err
		}
		total += len(content)
		if total > maxExtracted {
			return ErrArchiveInvalid
		}
		files[name] = content
		return nil
	})
	return files, err
}

func readPackFile(root *os.Root, name string, maximum int) ([]byte, error) {
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
	if !info.Mode().IsRegular() || info.Size() > int64(maximum) {
		return nil, ErrArchiveInvalid
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maximum {
		return nil, ErrArchiveInvalid
	}
	return content, nil
}

func sortedFileNames(files map[string][]byte) []string {
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths
}

func zipFiles(files map[string][]byte) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range sortedFileNames(files) {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, errors.Join(err, writer.Close())
		}
		if _, err := entry.Write(files[name]); err != nil {
			return nil, errors.Join(err, writer.Close())
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
