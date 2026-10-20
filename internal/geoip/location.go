package geoip

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

const countryCodes = " AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW "

func ValidCountry(code string) bool {
	return len(code) == 2 && strings.Contains(countryCodes, " "+code+" ")
}

func ValidVisibility(value string) bool {
	return value == "inherit" || value == "public" || value == "private"
}

const locationSelect = `SELECT id,mode,scope,country_code,visibility,provider,revision,resolved_at,last_attempt_at,next_refresh_at,error_code,source_encrypted IS NOT NULL FROM node_locations WHERE id=?`

func scanLocation(row rowScanner, now time.Time) (model.NodeLocation, error) {
	var value model.NodeLocation
	var resolved, attempt, next sql.NullTime
	err := row.Scan(&value.NodeID, &value.Mode, &value.Scope, &value.CountryCode, &value.Visibility, &value.Provider, &value.Revision, &resolved, &attempt, &next, &value.ErrorCode, &value.HasSource)
	value.ResolvedAt, value.LastAttemptAt, value.NextRefreshAt = nullableTime(resolved), nullableTime(attempt), nullableTime(next)
	value.Stale = resolved.Valid && now.Sub(resolved.Time) > 90*24*time.Hour
	return value, err
}

func (s *Service) Location(ctx context.Context, actor Actor, id string) (model.NodeLocation, error) {
	var value model.NodeLocation
	err := s.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		value, err = tx.location(id)
		return err
	})
	return value, err
}

func (tx transaction) location(id string) (model.NodeLocation, error) {
	value, err := scanLocation(tx.db.QueryRowContext(tx.ctx, locationSelect, id), tx.now)
	if !errors.Is(err, sql.ErrNoRows) {
		return value, err
	}
	var exists int
	if err = tx.db.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM nodes WHERE id=?`, id).Scan(&exists); err != nil {
		return value, ErrUnavailable
	}
	if exists == 0 {
		return value, ErrNotFound
	}
	return model.NodeLocation{NodeID: id, Mode: "observed", Visibility: "inherit", LocationPublic: model.LocationPublic{Scope: "invalid"}}, nil
}

func validateLocation(input model.LocationWrite) error {
	if !ValidVisibility(input.Visibility) {
		return ErrInvalid
	}
	switch input.Mode {
	case "manual":
		if !validManualLocation(input) {
			return ErrInvalid
		}
	case "managed_target":
		if input.ManagedTarget != "" && (!input.ConfirmManagedTarget || !validTarget(input.ManagedTarget)) {
			return ErrInvalid
		}
	case "observed", "disabled":
	default:
		return ErrInvalid
	}
	return nil
}

func validTarget(value string) bool {
	if len(value) == 0 || len(value) > 253 || !utf8.ValidString(value) || strings.ContainsAny(value, "/:@\\ \t\r\n\x00") {
		ip, _ := Classify(value)
		return ip.IsValid()
	}
	for _, label := range strings.Split(value, ".") {
		if !validTargetLabel(label) {
			return false
		}
	}

	return true
}

type LocationMutation struct {
	NodeID string
	model.LocationWrite
}

func (s *Service) SaveLocation(ctx context.Context, actor Actor, mutation LocationMutation) (model.NodeLocation, error) {
	id, input := mutation.NodeID, mutation.LocationWrite
	if err := validateLocation(input); err != nil {
		return model.NodeLocation{}, err
	}
	err := s.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, input.Mode == "managed_target", input.Mode == "managed_target"); err != nil {
			return err
		}
		previous, err := tx.location(id)
		if err != nil {
			return err
		}
		if _, err = tx.db.ExecContext(ctx, `INSERT INTO node_locations(id,updated_at) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, id, tx.now); err != nil {
			return ErrUnavailable
		}
		if err = tx.saveLocation(id, input, previous); err != nil {
			return err
		}
		return tx.audit(actor, "geoip.location.update", id)
	})
	if err != nil {
		return model.NodeLocation{}, err
	}
	return s.Location(ctx, actor, id)
}

func (tx transaction) saveLocation(id string, input model.LocationWrite, previous model.NodeLocation) error {
	if input.Mode == "managed_target" && input.ManagedTarget == "" {
		return tx.retainManagedTarget(id, input, previous)
	}
	if input.Mode == "disabled" && !input.Purge {
		return tx.disableLocation(id, input.Visibility)
	}
	var source []byte
	var fingerprint string
	if input.Mode == "managed_target" {
		var err error
		source, fingerprint, err = tx.sealTarget(id, input.ManagedTarget)
		if err != nil {
			return err
		}
	}

	if retainObserved(input, previous) {
		_, err := tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET visibility=?,updated_at=? WHERE id=?`, input.Visibility, tx.now, id)
		return err
	}
	scope, country, resolved, next := locationValues(input, tx.now)
	_, err := tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET mode=?,visibility=?,source_encrypted=?,source_fingerprint=?,scope=?,country_code=?,
 resolved_at=?,next_refresh_at=?,provider='',revision='',error_code='',failures=0,lease_hash='',lease_expires_at=NULL,updated_at=? WHERE id=?`, input.Mode, input.Visibility, source, fingerprint, scope, country, resolved, next, tx.now, id)
	if err != nil {
		return ErrUnavailable
	}
	if input.Purge {
		_, err = tx.db.ExecContext(tx.ctx, `DELETE FROM geoip_cache WHERE source_fingerprint NOT IN(SELECT source_fingerprint FROM node_locations WHERE source_fingerprint!='')`)
	}
	return err
}

func (tx transaction) disableLocation(id, visibility string) error {
	// mode绑定AAD；保留来源时先重新绑定disabled，后续observed报告再替换。
	var raw []byte
	var mode string
	if err := tx.db.QueryRowContext(tx.ctx, `SELECT source_encrypted,mode FROM node_locations WHERE id=?`, id).Scan(&raw, &mode); err != nil {
		return ErrUnavailable
	}
	if len(raw) > 0 {
		plain, err := tx.access.Open(raw, secretbox.GeoIPSourceAAD(id, mode))
		if err != nil {
			return ErrUnavailable
		}
		defer clear(plain)
		raw, err = tx.access.Seal(plain, secretbox.GeoIPSourceAAD(id, "disabled"))
		if err != nil {
			return ErrUnavailable
		}
	}
	_, err := tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET mode='disabled',visibility=?,source_encrypted=?,next_refresh_at=NULL,lease_hash='',lease_expires_at=NULL,updated_at=? WHERE id=?`, visibility, raw, tx.now, id)
	return err
}

func (s *Service) Refresh(ctx context.Context, actor Actor, id string) error {
	return s.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		value, err := tx.location(id)
		if err != nil {
			return err
		}
		if value.Mode == "manual" || value.Mode == "disabled" || !value.HasSource {
			return ErrInvalid
		}
		_, err = tx.db.ExecContext(ctx, `UPDATE node_locations SET next_refresh_at=?,failures=0 WHERE id=?`, tx.now, id)
		if err != nil {
			return ErrUnavailable
		}
		return tx.audit(actor, "geoip.location.refresh", id)
	})
}

func resolveTarget(ctx context.Context, target string) ([]string, error) {
	if ip, _ := Classify(target); ip.IsValid() {
		return []string{ip.String()}, nil
	}
	resolver := &net.Resolver{PreferGo: true}
	addresses, err := resolver.LookupNetIP(ctx, "ip", target)
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(addresses) == 0 || len(addresses) > 16 {
		return nil, ErrInvalid
	}
	out := make([]string, 0, len(addresses))
	for _, ip := range addresses {
		out = append(out, ip.Unmap().String())
	}
	return out, nil
}

func (tx transaction) sealTarget(id, target string) ([]byte, string, error) {
	raw := []byte(strings.ToLower(target))
	defer clear(raw)
	keyed, ok := tx.access.(secretbox.GeoIPFingerprinter)
	if !ok {
		return nil, "", ErrUnavailable
	}
	fingerprint, err := keyed.GeoIPFingerprint(raw)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	source, err := tx.access.Seal(raw, secretbox.GeoIPSourceAAD(id, "managed_target"))
	if err != nil {
		return nil, "", ErrUnavailable
	}
	return source, fingerprint, nil
}

func validManualLocation(input model.LocationWrite) bool {
	return ValidScope(input.Scope) && ((input.Scope == "public" && ValidCountry(input.CountryCode)) || (input.Scope != "public" && input.CountryCode == ""))
}

func validTargetLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-", r) {
			return false
		}
	}
	return true
}

func retainObserved(input model.LocationWrite, previous model.NodeLocation) bool {
	return input.Mode == "observed" && previous.Mode == "observed" && !input.Purge
}

func (tx transaction) retainManagedTarget(id string, input model.LocationWrite, previous model.NodeLocation) error {
	if previous.Mode != "managed_target" || input.Purge {
		return ErrInvalid
	}
	_, err := tx.db.ExecContext(tx.ctx, `UPDATE node_locations SET visibility=?,updated_at=? WHERE id=?`, input.Visibility, tx.now, id)
	return err
}

func locationValues(input model.LocationWrite, now time.Time) (scope, country string, resolved, next any) {
	scope = "invalid"
	if input.Mode == "manual" {
		scope, country, resolved = input.Scope, input.CountryCode, now
	}
	if input.Mode == "managed_target" {
		next = now
	}
	return scope, country, resolved, next
}
