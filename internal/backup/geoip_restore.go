package backup

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/model"
)

func validateGeoIPSnapshot(root string, manifest model.BackupManifest) (resultErr error) {
	database, err := openArchivedSQLite(filepath.Join(root, sqliteEntry))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	var edition, checksum string
	var size int64
	var epoch uint64
	err = database.QueryRow(`SELECT mmdb_edition,mmdb_sha256,mmdb_size,mmdb_build_epoch FROM geoip_settings WHERE id='global'`).Scan(&edition, &checksum, &size, &epoch)
	if isNoSuchTable(err) {
		if len(manifest.GeoIPFiles) != 0 {
			return errors.New("MMDB asset requires GeoIP snapshot schema")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if manifest.ExternalMMDB != "" {
		return nil
	}
	current, present := manifest.GeoIPFiles[geoIPCurrentEntry]
	if checksum == "" {
		if present {
			return errors.New("MMDB asset has no snapshot metadata")
		}
		return nil
	}
	if manifest.FormatVersion < 4 {
		return errors.New("managed MMDB requires backup format 4")
	}
	if !present || current.SHA256 != checksum || current.SizeBytes != size || current.BuildEpoch != epoch || current.Edition != edition {
		return errors.New("MMDB asset and SQLite snapshot disagree")
	}
	return nil
}

type geoIPRestoreTarget struct{ dataDir, token string }

func prepareGeoIPRestore(validated *validatedArchive, prepared *preparedRestore, input geoIPRestoreTarget) error {
	target := restoreTarget{live: filepath.Join(input.dataDir, "geoip"), new: filepath.Join(input.dataDir, "geoip.restore-new-"+input.token)}
	if err := os.Mkdir(target.new, 0o700); err != nil {
		return err
	}
	prepared.targets = append(prepared.targets, target)
	for name := range validated.manifest.GeoIPFiles {
		if !validGeoIPEntry(name) {
			return errors.New("unexpected restored GeoIP asset")
		}
		if err := copyFile(filepath.Join(validated.root, name), filepath.Join(target.new, filepath.Base(name)), 0o600); err != nil {
			return err
		}
	}
	directory, err := os.Open(target.new)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// reconcileGeoIPRestore 只修改隔离的恢复副本，不重写手工国家或生命周期事件。
func reconcileGeoIPRestore(ctx context.Context, path string, now time.Time) (resultErr error) {
	location := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, database.Close()) }()
	_, err = database.ExecContext(ctx, `UPDATE node_locations SET lease_hash='',lease_expires_at=NULL,worker_id='',next_refresh_at=?
 WHERE mode IN('observed','managed_target') AND lease_hash<>''`, now)
	if isNoSuchTable(err) {
		return nil
	}
	return err
}
