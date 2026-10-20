package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"sync"

	"github.com/oschwald/maxminddb-golang/v2"

	"github.com/beat/backend/internal/model"
)

const MaximumMMDBBytes = 128 * 1024 * 1024

type Database struct {
	mu     sync.RWMutex
	reader *maxminddb.Reader
}

func (db *Database) Lookup(source string) (string, error) {
	ip, scope := Classify(source)
	if scope != "public" {
		return "", ErrInvalid
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.reader == nil {
		return "", ErrUnavailable
	}
	return lookupCountry(db.reader, ip)
}

func lookupCountry(reader *maxminddb.Reader, ip netip.Addr) (string, error) {
	var record struct {
		Country struct {
			Code string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	result := reader.Lookup(ip)
	if err := result.Err(); err != nil {
		return "", ErrUnavailable
	}
	if !result.Found() {
		return "", nil
	}
	if err := result.Decode(&record); err != nil {
		return "", ErrUnavailable
	}
	if record.Country.Code != "" && !ValidCountry(record.Country.Code) {
		return "", ErrUnavailable
	}
	return record.Country.Code, nil
}

func (db *Database) replace(reader *maxminddb.Reader) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	previous := db.reader
	db.reader = reader
	if previous != nil {
		return previous.Close()
	}
	return nil
}

func (db *Database) Close() error { return db.replace(nil) }

func validateDatabase(path string) (*maxminddb.Reader, model.MMDBMetadata, error) {
	var metadata model.MMDBMetadata
	file, err := os.Open(path)
	if err != nil {
		return nil, metadata, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaximumMMDBBytes {
		return nil, metadata, ErrInvalid
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, MaximumMMDBBytes+1)); err != nil {
		return nil, metadata, ErrUnavailable
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, metadata, ErrInvalid
	}
	if err = validateReader(reader); err != nil {
		_ = reader.Close()
		return nil, metadata, err
	}

	metadata = model.MMDBMetadata{Edition: reader.Metadata.DatabaseType, BuildEpoch: uint64(reader.Metadata.BuildEpoch), SHA256: hex.EncodeToString(hash.Sum(nil)), SizeBytes: info.Size(), State: "ready"}
	return reader, metadata, nil
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func validateReader(reader *maxminddb.Reader) error {
	if reader.Metadata.DatabaseType != "GeoLite2-Country" && reader.Metadata.DatabaseType != "GeoIP2-Country" {
		return ErrInvalid
	}
	if err := reader.Verify(); err != nil {
		return ErrInvalid
	}
	if _, err := lookupCountry(reader, netip.MustParseAddr("1.1.1.1")); err != nil {
		return ErrInvalid
	}
	return nil
}
