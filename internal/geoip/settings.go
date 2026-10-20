package geoip

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type rowScanner interface{ Scan(...any) error }

const settingsSelect = `SELECT provider,revision,remote_consent,credential_encrypted IS NOT NULL,mmdb_source,mmdb_edition,mmdb_build_epoch,mmdb_sha256,mmdb_size,mmdb_state,mmdb_error FROM geoip_settings WHERE id='global'`

func scanSettings(row rowScanner) (model.GeoIPSettings, error) {
	var value model.GeoIPSettings
	err := row.Scan(&value.Provider, &value.Revision, &value.RemoteConsent, &value.HasCredential, &value.MMDB.Source, &value.MMDB.Edition, &value.MMDB.BuildEpoch, &value.MMDB.SHA256, &value.MMDB.SizeBytes, &value.MMDB.State, &value.MMDB.ErrorCode)
	return value, err
}

func (s *Service) Settings(ctx context.Context, actor Actor) (model.GeoIPSettings, error) {
	var value model.GeoIPSettings
	err := s.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		value, err = scanSettings(tx.db.QueryRowContext(ctx, settingsSelect))
		return err
	})
	return value, err
}

func remoteProvider(provider string) bool {
	return provider == "geojs_https" || provider == "ipinfo_https" || provider == "ip_api_pro_https"
}

func validateSettings(value model.GeoIPSettingsWrite) error {
	if value.Provider != "disabled" && value.Provider != "mmdb" && !remoteProvider(value.Provider) {
		return ErrInvalid
	}
	if remoteProvider(value.Provider) && !value.RemoteConsent {
		return ErrInvalid
	}
	if value.Credential != nil && (len(*value.Credential) > 4096 || strings.ContainsAny(*value.Credential, "\x00\r\n")) {
		return ErrInvalid
	}
	return nil
}

func (s *Service) SaveSettings(ctx context.Context, actor Actor, input model.GeoIPSettingsWrite) (model.GeoIPSettings, error) {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if err := validateSettings(input); err != nil {
		return model.GeoIPSettings{}, err
	}
	err := s.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, true, true); err != nil {
			return err
		}
		current, err := scanSettings(tx.db.QueryRowContext(ctx, settingsSelect))
		if err != nil {
			return ErrUnavailable
		}
		if current.Revision != input.ExpectedRevision {
			return ErrConflict
		}
		return tx.saveSettings(actor, input, current)
	})
	if err != nil {
		return model.GeoIPSettings{}, err
	}
	return s.Settings(ctx, actor)
}

func (tx transaction) saveSettings(actor Actor, input model.GeoIPSettingsWrite, current model.GeoIPSettings) error {
	if err := tx.saveCredential(input, current); err != nil {
		return err
	}
	if _, err := tx.db.ExecContext(tx.ctx, `UPDATE geoip_settings SET provider=?,remote_consent=?,revision=revision+1,updated_at=? WHERE id='global'`, input.Provider, remoteProvider(input.Provider) && input.RemoteConsent, tx.now); err != nil {
		return ErrUnavailable
	}
	if _, err := tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET lease_hash='',lease_expires_at=NULL,next_refresh_at=CASE WHEN ?='disabled' THEN NULL ELSE ? END WHERE mode IN('observed','managed_target')`, input.Provider, tx.now); err != nil {
		return ErrUnavailable
	}
	if input.Purge {
		if _, err := tx.db.ExecContext(tx.ctx, `DELETE FROM geoip_cache`); err != nil {
			return ErrUnavailable
		}
		if _, err := tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET source_encrypted=NULL,source_fingerprint='',country_code='',resolved_at=NULL,scope='invalid',next_refresh_at=NULL WHERE mode!='manual'`); err != nil {
			return ErrUnavailable
		}
	}
	return tx.audit(actor, "geoip.settings.update", "global")
}

func providerRevision(settings model.GeoIPSettings) string {
	return fmt.Sprintf("%s:%d:%s", settings.Provider, settings.Revision, settings.MMDB.SHA256)
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func (tx transaction) saveCredential(input model.GeoIPSettingsWrite, current model.GeoIPSettings) error {
	var credential []byte
	if input.Credential != nil && *input.Credential != "" {
		var err error
		credential, err = tx.access.Seal([]byte(*input.Credential), secretbox.GeoIPCredentialAAD())
		if err != nil {
			return ErrUnavailable
		}
	}
	if !validProviderCredential(input, current) {
		return ErrInvalid
	}
	if input.Credential != nil || current.Provider != input.Provider {
		if _, err := tx.db.ExecContext(tx.ctx, `UPDATE geoip_settings SET credential_encrypted=? WHERE id='global'`, credential); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func validProviderCredential(input model.GeoIPSettingsWrite, current model.GeoIPSettings) bool {
	if input.Provider != "ip_api_pro_https" {
		return true
	}
	if input.Credential != nil {
		return *input.Credential != ""
	}
	return current.HasCredential && current.Provider == input.Provider
}
