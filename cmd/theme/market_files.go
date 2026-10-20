package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/theme"
)

const (
	marketKeyFileLimit = 512
	marketEntriesLimit = 64 << 10
	marketArchiveLimit = 25 << 20
)

type marketOutput struct {
	name string
	body []byte
}

type marketInput struct {
	Archive    string `json:"archive"`
	PackageURL string `json:"package_url"`
}

func writeMarketOutputs(outputs []marketOutput) error {
	created := []string{}
	for _, output := range outputs {
		if err := writeNewMarketFile(output); err != nil {
			for _, name := range created {
				err = errors.Join(err, os.Remove(name))
			}
			return err
		}
		created = append(created, output.name)
	}
	return nil
}

func writeNewMarketFile(output marketOutput) error {
	if err := os.MkdirAll(filepath.Dir(output.name), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(output.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(output.body)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return errors.Join(err, os.Remove(output.name))
	}
	return nil
}

func readMarketFile(filename string, limit int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("market input must be a bounded regular file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("market input exceeds its size limit")
	}
	return raw, nil
}

func readMarketPrivateFile(filename string) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private key file must have owner-only permissions")
	}
	return readMarketFile(filename, marketKeyFileLimit)
}

func readMarketInputs(filename string) ([]theme.MarketPackageInput, error) {
	raw, err := readMarketFile(filename, marketEntriesLimit)
	if err != nil {
		return nil, err
	}
	var entries []marketInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("invalid market entry list: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("market entry list contains trailing data")
	}
	if entries == nil || len(entries) > 200 {
		return nil, fmt.Errorf("market entry list must contain at most 200 packages")
	}
	packages := make([]theme.MarketPackageInput, 0, len(entries))
	for _, entry := range entries {
		archive := entry.Archive
		if !filepath.IsAbs(archive) {
			archive = filepath.Join(filepath.Dir(filename), archive)
		}
		body, err := readMarketFile(archive, marketArchiveLimit)
		if err != nil {
			return nil, err
		}
		packages = append(packages, theme.MarketPackageInput{Archive: body, URL: entry.PackageURL})
	}
	return packages, nil
}
