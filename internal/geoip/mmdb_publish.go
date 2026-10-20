package geoip

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

type publicationJournal struct {
	NewChecksum string `json:"new_checksum"`
	OldChecksum string `json:"old_checksum"`
	HadPrevious bool   `json:"had_previous"`
}

func (s *Service) publishDatabase(staged string) (func() error, error) {
	journal, err := s.preparePublication(staged)
	if err != nil {
		return nil, err
	}
	rollback := func() error { return s.recoverPublication(journal, false) }
	current, previous := s.assetPath("country.mmdb"), s.assetPath("country.mmdb.previous")
	if journal.HadPrevious {
		if err = os.Rename(previous, s.assetPath(".previous.mmdb.pending")); err != nil {
			return nil, errors.Join(ErrUnavailable, rollback())
		}
	}
	if journal.OldChecksum != "" {
		if err = os.Rename(current, previous); err != nil {
			return nil, errors.Join(ErrUnavailable, rollback())
		}
	}
	if err = os.Rename(staged, current); err != nil {
		return nil, errors.Join(ErrUnavailable, rollback())
	}
	if err = syncDirectory(s.directory); err != nil {
		return nil, errors.Join(ErrUnavailable, rollback())
	}
	return rollback, nil
}

func (s *Service) preparePublication(staged string) (publicationJournal, error) {
	var journal publicationJournal
	metadata, err := ValidateAsset(staged)
	if err != nil {
		return journal, err
	}
	journal.NewChecksum = metadata.SHA256
	current, err := s.backupAsset("country.mmdb")
	if err != nil {
		return journal, err
	}
	if current != nil {
		journal.OldChecksum = current.Metadata.SHA256
	}
	previous, err := s.backupAsset("country.mmdb.previous")
	if err != nil {
		return journal, err
	}
	journal.HadPrevious = previous != nil
	encoded, err := json.Marshal(journal)
	if err != nil {
		return journal, err
	}
	file, err := os.OpenFile(s.assetPath(".publication.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return journal, ErrUnavailable
	}
	_, writeErr := file.Write(encoded)
	err = errors.Join(writeErr, file.Sync(), file.Close(), syncDirectory(s.directory))
	if err != nil {
		return journal, ErrUnavailable
	}
	return journal, nil
}

func (s *Service) assetPath(name string) string { return filepath.Join(s.directory, name) }

func removeTemporary(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Service) finishPublication() error {
	if err := removeTemporary(s.assetPath(".previous.mmdb.pending")); err != nil {
		return ErrUnavailable
	}
	if err := removeTemporary(s.assetPath(".publication.json")); err != nil {
		return ErrUnavailable
	}
	return syncDirectory(s.directory)
}

func (s *Service) recoverPublication(journal publicationJournal, committed bool) error {
	if committed {
		return s.finishPublication()
	}
	if err := s.restoreCurrent(journal); err != nil {
		return err
	}
	var err error

	if _, err = os.Lstat(s.assetPath(".previous.mmdb.pending")); err == nil {
		if err = os.Rename(s.assetPath(".previous.mmdb.pending"), s.assetPath("country.mmdb.previous")); err != nil {
			return ErrUnavailable
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrUnavailable
	}
	return s.finishPublication()
}

func (s *Service) RecoverPublication() error {
	if s.external != "" {
		return nil
	}
	journal, err := s.readPublication()
	if err != nil {
		return err
	}
	if journal == nil {
		return nil
	}

	var checksum string
	if err = s.db.QueryRow(`SELECT mmdb_sha256 FROM geoip_settings WHERE id='global'`).Scan(&checksum); err != nil {
		return ErrUnavailable
	}
	current, err := s.backupAsset("country.mmdb")
	if err != nil {
		return err
	}
	committed := checksum == journal.NewChecksum && current != nil && current.Metadata.SHA256 == journal.NewChecksum
	return s.recoverPublication(*journal, committed)
}

func (s *Service) restoreCurrent(journal publicationJournal) error {
	current, err := s.backupAsset("country.mmdb")
	if err != nil {
		return err
	}
	if journal.OldChecksum == "" {
		if current != nil && current.Metadata.SHA256 == journal.NewChecksum {
			err = removeTemporary(s.assetPath("country.mmdb"))
		}
	} else if !sameAssetChecksum(current, journal.OldChecksum) {
		old, readErr := s.backupAsset("country.mmdb.previous")
		if readErr != nil || old == nil || old.Metadata.SHA256 != journal.OldChecksum {
			return ErrUnavailable
		}
		err = os.Rename(s.assetPath("country.mmdb.previous"), s.assetPath("country.mmdb"))
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) readPublication() (*publicationJournal, error) {
	file, err := os.Open(s.assetPath(".publication.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return nil, ErrUnavailable
	}
	var journal publicationJournal
	if err = json.Unmarshal(raw, &journal); err != nil || len(journal.NewChecksum) != 64 {
		return nil, ErrUnavailable
	}
	return &journal, nil
}

func sameAssetChecksum(asset *BackupAsset, checksum string) bool {
	return asset != nil && asset.Metadata.SHA256 == checksum
}
