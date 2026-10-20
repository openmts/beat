package geoip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

type MMDBUpdate struct {
	AccountID        string `json:"account_id"`
	LicenseKey       string `json:"license_key"`
	Edition          string `json:"edition"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func validUpdate(input MMDBUpdate) bool {
	return (input.Edition == "GeoLite2-Country" || input.Edition == "GeoIP2-Country") && input.AccountID != "" && len(input.AccountID) <= 128 && input.LicenseKey != "" && len(input.LicenseKey) <= 4096 && !strings.ContainsAny(input.AccountID+input.LicenseKey, "\r\n\x00")
}

func (s *Service) UpdateMMDB(ctx context.Context, actor Actor, input MMDBUpdate) (model.GeoIPSettings, error) {
	if !validUpdate(input) {
		return model.GeoIPSettings{}, ErrInvalid
	}
	if s.external != "" {
		return model.GeoIPSettings{}, ErrForbidden
	}
	if err := s.write(ctx, func(tx transaction) error { return tx.authorize(actor, true, true) }); err != nil {
		return model.GeoIPSettings{}, err
	}
	if err := privateDirectory(s.directory); err != nil {
		return model.GeoIPSettings{}, err
	}
	archive, err := s.downloadDatabase(ctx, input)
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	defer func() { _ = os.Remove(archive) }()
	database, err := extractDatabase(archive, s.directory)
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	defer func() { _ = os.Remove(database) }()
	file, err := os.Open(database)
	if err != nil {
		return model.GeoIPSettings{}, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	return s.InstallMMDB(ctx, actor, MMDBUpload{Body: file, ExpectedRevision: input.ExpectedRevision, source: "official"})
}

func (s *Service) vendorRequest(ctx context.Context, input MMDBUpdate, suffix string) (*http.Response, error) {
	endpoint := "https://download.maxmind.com/geoip/databases/" + input.Edition + "/download?suffix=" + suffix
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.SetBasicAuth(input.AccountID, input.LicenseKey)
	response, err := s.client.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, ErrUnavailable
	}
	return response, nil
}

func (s *Service) downloadDatabase(ctx context.Context, input MMDBUpdate) (string, error) {
	response, err := s.vendorRequest(ctx, input, "tar.gz.sha256")
	if err != nil {
		return "", err
	}
	checksum, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(checksum) > 4096 {
		return "", ErrUnavailable
	}
	expected, err := parseChecksum(checksum)
	if err != nil {
		return "", err
	}

	response, err = s.vendorRequest(ctx, input, "tar.gz")
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	hash := sha256.New()
	path, err := s.stageDatabase(io.TeeReader(response.Body, hash))
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(hex.EncodeToString(expected), hex.EncodeToString(hash.Sum(nil))) {
		_ = os.Remove(path)
		return "", ErrInvalid
	}
	return path, nil
}

func (s *Service) RollbackMMDB(ctx context.Context, actor Actor, revision int64) (model.GeoIPSettings, error) {
	if s.external != "" {
		return model.GeoIPSettings{}, ErrForbidden
	}
	if err := s.write(ctx, func(tx transaction) error { return tx.authorize(actor, true, true) }); err != nil {
		return model.GeoIPSettings{}, err
	}
	file, err := os.Open(filepath.Join(s.directory, "country.mmdb.previous"))
	if err != nil {
		return model.GeoIPSettings{}, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	return s.InstallMMDB(ctx, actor, MMDBUpload{Body: file, ExpectedRevision: revision, source: "rollback"})
}

func parseChecksum(checksum []byte) ([]byte, error) {
	fields := strings.Fields(string(checksum))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return nil, ErrInvalid
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil {
		return nil, ErrInvalid
	}
	return expected, nil
}
