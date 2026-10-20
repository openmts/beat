package agentrollout

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"
)

type PageQuery struct {
	Limit     int
	Cursor    string
	RolloutID string
}

type Page[T any] struct {
	ServerTime *time.Time `json:"server_time,omitempty"`
	Items      []T        `json:"items"`
	NextCursor string     `json:"next_cursor"`
}

type ArtifactMetadata struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ReleaseDetail struct {
	Release
	Artifacts []ArtifactMetadata `json:"artifacts"`
}

// 元数据读取复验owner会话，不要求recent proof，也不记录敏感载荷。
func (s *Service) read(ctx context.Context, actor Actor, action func(*transaction) error) error {
	if err := s.requireEnabled(); err != nil {
		return err
	}
	return s.readOwner(ctx, actor, action)
}

func (s *Service) readOwner(ctx context.Context, actor Actor, action func(*transaction) error) error {
	db, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, actor: actor, at: s.now()}
	if err := tx.authority(false); err != nil {
		return err
	}
	if err := action(tx); err != nil {
		return err
	}
	tx.at = s.now()
	if err := tx.authority(false); err != nil {
		return err
	}
	if err := db.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func pageBounds(query PageQuery) (int, string, error) {
	limit := query.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(query.Cursor) > 512 {
		return 0, "", ErrInvalid
	}
	cursor, err := base64.RawURLEncoding.DecodeString(query.Cursor)
	if err != nil || len(cursor) > maximumIdentifierBytes || strings.ContainsRune(string(cursor), 0) {
		return 0, "", ErrInvalid
	}
	return limit, string(cursor), nil
}

func (s *Service) ReleaseDetail(ctx context.Context, actor Actor, id string) (ReleaseDetail, error) {
	var value ReleaseDetail
	err := s.read(ctx, actor, func(tx *transaction) error {
		var err error
		value.Release, err = tx.releaseMetadata(id)
		if err != nil {
			return err
		}
		rows, err := tx.db.QueryContext(ctx, "SELECT artifact_id,state,size,digest FROM agent_release_artifacts WHERE release_id=? ORDER BY artifact_id LIMIT ?", id, len(value.Manifest.Artifacts)+1)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = rows.Close() }()
		value.Artifacts = []ArtifactMetadata{}
		for rows.Next() {
			var item ArtifactMetadata
			if err := rows.Scan(&item.ID, &item.State, &item.Size, &item.SHA256); err != nil {
				return ErrUnavailable
			}
			value.Artifacts = append(value.Artifacts, item)
			if len(value.Artifacts) > len(value.Manifest.Artifacts) {
				return ErrUnavailable
			}
		}
		return rows.Err()
	})
	return value, err
}

func (tx *transaction) releaseMetadata(id string) (Release, error) {
	stored, err := tx.loadRelease(id)
	if err != nil {
		return Release{}, err
	}
	manifest, err := decodeManifest(stored.signed.Payload)
	if err != nil || !stored.matches(manifest, id) {
		return Release{}, ErrManifest
	}
	return Release{ID: id, Manifest: manifest, Verified: stored.release.Verified, Revoked: stored.revoked.Valid}, nil
}

func (s *Service) GetRollout(ctx context.Context, actor Actor, id string) (Rollout, error) {
	var value Rollout
	err := s.read(ctx, actor, func(tx *transaction) error {
		var err error
		value, err = tx.rollout(id)
		value.ServerTime = &tx.at
		return err
	})
	return value, err
}

func (s *Service) ListReleases(ctx context.Context, actor Actor, query PageQuery) (Page[Release], error) {
	return listMetadata(s, ctx, metadataQuery[Release]{actor: actor, page: query, table: "agent_releases", load: func(tx *transaction, id string) (Release, error) { return tx.releaseMetadata(id) }})
}

func (s *Service) ListRollouts(ctx context.Context, actor Actor, query PageQuery) (Page[Rollout], error) {
	return listMetadata(s, ctx, metadataQuery[Rollout]{actor: actor, page: query, table: "agent_rollouts", load: func(tx *transaction, id string) (Rollout, error) { return tx.rollout(id) }})
}

type metadataQuery[T any] struct {
	actor Actor
	page  PageQuery
	table string
	load  func(*transaction, string) (T, error)
}

func listMetadata[T any](s *Service, ctx context.Context, query metadataQuery[T]) (Page[T], error) {
	result := Page[T]{Items: []T{}}
	limit, cursor, err := pageBounds(query.page)
	if err != nil {
		return result, err
	}
	err = s.read(ctx, query.actor, func(tx *transaction) error {
		ids, err := tx.metadataIDs(query.table, cursor, limit+1)
		if err != nil {
			return err
		}
		if len(ids) > limit {
			ids = ids[:limit]
			result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(ids[limit-1]))
		}
		for _, id := range ids {
			item, err := query.load(tx, id)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
		}
		return nil
	})
	return result, err
}

func (tx *transaction) metadataIDs(table, cursor string, limit int) ([]string, error) {
	// table仅来自上面的固定包内调用点。
	rows, err := tx.db.QueryContext(tx.ctx, "SELECT id FROM "+table+" WHERE id>? ORDER BY id LIMIT ?", cursor, limit)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, ErrUnavailable
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) ListTargets(ctx context.Context, actor Actor, query PageQuery) (Page[Target], error) {
	result := Page[Target]{Items: []Target{}}
	limit, cursor, err := pageBounds(query)
	if err != nil {
		return result, err
	}
	err = s.read(ctx, actor, func(tx *transaction) error {
		result.ServerTime = &tx.at
		if _, err := tx.rollout(query.RolloutID); err != nil {
			return err
		}
		rows, err := tx.db.QueryContext(ctx, "SELECT id,node_snapshot_id,node_name,source_version,os,arch,wave,state,reason,healthy_since,last_report_at,lease_expires_at FROM agent_rollout_targets WHERE rollout_id=? AND id>? ORDER BY id LIMIT ?", query.RolloutID, cursor, limit+1)
		if err != nil {
			return ErrUnavailable
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var item Target
			if err := rows.Scan(&item.ID, &item.NodeID, &item.Name, &item.Version, &item.OS, &item.Arch, &item.Wave, &item.State, &item.Reason, &item.HealthySince, &item.LastReportAt, &item.LeaseExpiresAt); err != nil {
				return ErrUnavailable
			}
			result.Items = append(result.Items, item)
		}
		return rows.Err()
	})
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(result.Items[limit-1].ID))
	}
	return result, err
}
