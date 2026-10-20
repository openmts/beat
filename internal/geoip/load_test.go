package geoip_test

import (
	"fmt"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

func TestTenThousandNodeQueueClaimsBoundedWork(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.SaveSettings(t.Context(), f.actor, model.GeoIPSettingsWrite{Provider: "geojs_https", RemoteConsent: true, ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	err := f.secrets.WithWrite(t.Context(), func(access secretbox.ResourceAccess) error {
		tx, err := f.db.DB.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		_, err = tx.Exec(`WITH RECURSIVE items(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM items WHERE x<10000)
 INSERT INTO nodes(id,name,host,port,created_at,updated_at) SELECT printf('load-%05d',x),printf('Load %d',x),'',22,?,? FROM items`, f.now, f.now)
		if err != nil {
			return err
		}
		insert, err := tx.Prepare(`INSERT INTO node_locations(id,source_encrypted,source_fingerprint,scope,next_refresh_at,updated_at) VALUES(?,?,?,'private',?,?)`)
		if err != nil {
			return err
		}
		defer func() { _ = insert.Close() }()
		fingerprint, err := access.(secretbox.GeoIPFingerprinter).GeoIPFingerprint([]byte("10.0.0.1"))
		if err != nil {
			return err
		}
		for index := 1; index <= 10000; index++ {
			id := fmt.Sprintf("load-%05d", index)
			encrypted, err := access.Seal([]byte("10.0.0.1"), secretbox.GeoIPSourceAAD(id, "observed"))
			if err != nil {
				return err
			}
			if _, err = insert.Exec(id, encrypted, fingerprint, f.now, f.now); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Process(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.service.Snapshot(t.Context())
	if err != nil || snapshot.Due != 9999 || snapshot.Leased != 0 || snapshot.CacheEntries != 1 || snapshot.OldestDueAt == nil {
		t.Fatal(snapshot, err)
	}
}
