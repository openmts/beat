package store

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func encodeRawBackupCursor(t *testing.T, payload string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func seedBackupPage(t *testing.T, store *BackupStore, now time.Time, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		record := model.BackupRecord{
			ID: fmt.Sprintf("backup-%03d", index), Filename: fmt.Sprintf("beat-%03d.zip", index),
			Source: "generated", State: model.BackupStateReady,
			CreatedAt: now.Add(-time.Duration(index) * time.Second),
		}
		if err := store.Create(t.Context(), &record); err != nil {
			t.Fatalf("create backup record %d: %v", index, err)
		}
	}
}

func TestListBackupsPageWalksEveryRecordOnce(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewBackupStore(sqlite.DB)
	seedBackupPage(t, store, time.Now().UTC(), 7)
	seen := map[string]int{}
	cursor := ""
	for page := 0; page < 10; page++ {
		result, err := store.ListBackupsPage(t.Context(), BackupQuery{Cursor: cursor, Limit: 3})
		if err != nil {
			t.Fatalf("list page %d: %v", page, err)
		}
		if len(result.Items) > 3 {
			t.Fatalf("page %d returned %d items", page, len(result.Items))
		}
		for _, record := range result.Items {
			seen[record.ID]++
		}
		if result.NextCursor == "" {
			if len(seen) != 7 {
				t.Fatalf("walked %d records, want 7", len(seen))
			}
			for id, count := range seen {
				if count != 1 {
					t.Fatalf("record %s returned %d times", id, count)
				}
			}
			return
		}
		cursor = result.NextCursor
	}
	t.Fatal("cursor walk did not terminate")
}

// TestListBackupsPageKeepsSubMillisecondTies 覆盖同一毫秒内的记录不被静默跳过。
func TestListBackupsPageKeepsSubMillisecondTies(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewBackupStore(sqlite.DB)
	base := time.Now().UTC().Truncate(time.Millisecond)
	for index, offset := range []time.Duration{0, 300 * time.Microsecond, 900 * time.Microsecond} {
		record := model.BackupRecord{ID: fmt.Sprintf("tie-%d", index),
			Filename: fmt.Sprintf("beat-tie-%d.zip", index), Source: "generated",
			State: model.BackupStateReady, CreatedAt: base.Add(offset)}
		if err := store.Create(t.Context(), &record); err != nil {
			t.Fatalf("create tie record: %v", err)
		}
	}
	seen := 0
	cursor := ""
	for page := 0; page < 5; page++ {
		result, err := store.ListBackupsPage(t.Context(), BackupQuery{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("list tie page: %v", err)
		}
		seen += len(result.Items)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	if seen != 3 {
		t.Fatalf("sub-millisecond walk returned %d records, want 3", seen)
	}
}

func TestListBackupsPageBindsStateFilterToCursor(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewBackupStore(sqlite.DB)
	now := time.Now().UTC()
	seedBackupPage(t, store, now, 3)
	failed := model.BackupRecord{ID: "failed", Filename: "beat-failed.zip", Source: "uploaded",
		State: model.BackupStateFailed, CreatedAt: now.Add(time.Minute)}
	if err := store.Create(t.Context(), &failed); err != nil {
		t.Fatalf("create failed record: %v", err)
	}
	page, err := store.ListBackupsPage(t.Context(), BackupQuery{State: model.BackupStateReady, Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("filtered page = %#v, %v", page.Items, err)
	}
	for _, record := range page.Items {
		if record.State != model.BackupStateReady {
			t.Fatalf("filtered page leaked state %s", record.State)
		}
	}
	if _, err := store.ListBackupsPage(t.Context(),
		BackupQuery{State: model.BackupStateFailed, Cursor: page.NextCursor}); err != ErrInvalidBackupQuery {
		t.Fatalf("cursor reused across filters: %v", err)
	}
	rest, err := store.ListBackupsPage(t.Context(),
		BackupQuery{State: model.BackupStateReady, Cursor: page.NextCursor})
	if err != nil || len(rest.Items) != 1 || rest.NextCursor != "" {
		t.Fatalf("filtered remainder = %#v, %v", rest.Items, err)
	}
}

func TestListBackupsPageRejectsInvalidQuery(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewBackupStore(sqlite.DB)
	seedBackupPage(t, store, time.Now().UTC(), 1)
	cases := []BackupQuery{
		{Limit: -1}, {Limit: BackupMaximumPage + 1}, {State: "unknown"},
		{Cursor: "not-base64!"}, {Cursor: encodeRawBackupCursor(t, `{"v":9,"at":"2026-01-01T00:00:00Z","id":"a"}`)},
		{Cursor: encodeRawBackupCursor(t, `{"v":1,"at":"whenever","id":"a"}`)},
		{Cursor: encodeRawBackupCursor(t, `{"v":1,"at":"2026-01-01T00:00:00Z"}`)},
	}
	for index, query := range cases {
		if _, err := store.ListBackupsPage(t.Context(), query); err != ErrInvalidBackupQuery {
			t.Fatalf("case %d returned %v", index, err)
		}
	}
}

func TestListBackupsPageUsesDefaultPageSize(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewBackupStore(sqlite.DB)
	seedBackupPage(t, store, time.Now().UTC(), BackupDefaultPage+10)
	page, err := store.ListBackupsPage(t.Context(), BackupQuery{})
	if err != nil || len(page.Items) != BackupDefaultPage || page.NextCursor == "" {
		t.Fatalf("default page = %d items, cursor %q, %v", len(page.Items), page.NextCursor, err)
	}
}
