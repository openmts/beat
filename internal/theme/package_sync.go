package theme

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func syncPackageParents(root *os.Root) error {
	return errors.Join(syncRootDirectory(root, "packages"), syncRootDirectory(root, "quarantine"))
}

func syncRootDirectory(root *os.Root, name string) error {
	if err := rejectAssetLinks(root, name); err != nil {
		return err
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}

func syncThemeDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}

func syncStagedTheme(staging string) error {
	directories := []string{}
	err := filepath.WalkDir(staging, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(directories)))
	for _, directory := range directories {
		if err := syncThemeDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}
