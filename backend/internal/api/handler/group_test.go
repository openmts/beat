package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func setupTestDB(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestHandleListGroups(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	groupStore := store.NewGroupStore(s.DB)
	h := NewGroupHandler(groupStore)

	_, _ = groupStore.CreateGroup(ctx, "Group A")
	_, _ = groupStore.CreateGroup(ctx, "Group B")

	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListGroups(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleCreateGroup(t *testing.T) {
	t.Run("creates group", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		body := `{"name": "New Group"}`
		req := httptest.NewRequest(http.MethodPost, "/api/groups", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateGroup(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("empty name returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		body := `{"name": ""}`
		req := httptest.NewRequest(http.MethodPost, "/api/groups", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateGroup(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/api/groups", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleCreateGroup(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleUpdateGroup(t *testing.T) {
	t.Run("updates group", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		g, err := groupStore.CreateGroup(ctx, "Old Name")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := `{"name": "New Name"}`
		req := httptest.NewRequest(http.MethodPut, "/api/groups/"+g.ID, strings.NewReader(body))
		req = req.WithContext(ctx)
		req.SetPathValue("id", g.ID)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleUpdateGroup(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		body := `not-json`
		req := httptest.NewRequest(http.MethodPut, "/api/groups/some-id", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.SetPathValue("id", "some-id")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleUpdateGroup(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("non-existent group returns 404", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		body := `{"name": "New Name"}`
		req := httptest.NewRequest(http.MethodPut, "/api/groups/nonexistent", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.SetPathValue("id", "nonexistent")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleUpdateGroup(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})
}

func TestHandleDeleteGroup(t *testing.T) {
	t.Run("deletes group", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		g, err := groupStore.CreateGroup(ctx, "To Delete")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/groups/"+g.ID, nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", g.ID)
		w := httptest.NewRecorder()

		h.HandleDeleteGroup(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", w.Code)
		}
	})

	t.Run("deleting default group returns 403", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		defaultGroup, err := groupStore.GetDefaultGroup(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/groups/"+defaultGroup.ID, nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", defaultGroup.ID)
		w := httptest.NewRecorder()

		h.HandleDeleteGroup(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", w.Code)
		}
	})
}

func TestHandleUpdateSortOrder(t *testing.T) {
	t.Run("updates sort order", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		g1, _ := groupStore.CreateGroup(ctx, "Group 1")
		g2, _ := groupStore.CreateGroup(ctx, "Group 2")

		body := `{"ids":["` + g2.ID + `","` + g1.ID + `"]}`
		req := httptest.NewRequest(http.MethodPut, "/api/groups/sort-order", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleUpdateSortOrder(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s := setupTestDB(t)
		ctx := context.Background()
		groupStore := store.NewGroupStore(s.DB)
		h := NewGroupHandler(groupStore)

		body := `not-json`
		req := httptest.NewRequest(http.MethodPut, "/api/groups/sort-order", strings.NewReader(body))
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleUpdateSortOrder(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleSetDefaultGroup(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	groupStore := store.NewGroupStore(s.DB)
	h := NewGroupHandler(groupStore)

	g, err := groupStore.CreateGroup(ctx, "New Default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/groups/"+g.ID+"/default", nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", g.ID)
	w := httptest.NewRecorder()

	h.HandleSetDefaultGroup(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
