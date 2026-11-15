package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"beat/internal/platform/sqlite"
	"beat/internal/publictheme"
)

type publicThemeEnvelope struct {
	Document    publictheme.Document `json:"document"`
	Revision    int                  `json:"revision"`
	PublishedAt *time.Time           `json:"publishedAt,omitempty"`
}

type adminThemeEnvelope struct {
	Document          publictheme.Document `json:"document"`
	Revision          int                  `json:"revision"`
	UpdatedAt         *time.Time           `json:"updatedAt,omitempty"`
	PublishedRevision int                  `json:"publishedRevision"`
	PublishedAt       *time.Time           `json:"publishedAt,omitempty"`
	HasDraft          bool                 `json:"hasDraft"`
}

type publicThemeRevisionItem struct {
	Revision    int        `json:"revision"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

func (s *Server) handleThemeCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": publictheme.Presets()})
}

func (s *Server) publishedTheme(ctx context.Context) (publicThemeEnvelope, error) {
	revision, err := s.store.GetPublicThemeRevision(ctx, "published")
	if errors.Is(err, sqlite.ErrPublicThemeNotFound) {
		doc := publictheme.DefaultDocument()
		return publicThemeEnvelope{Document: doc, Revision: doc.Revision}, nil
	}
	if err != nil {
		return publicThemeEnvelope{}, err
	}
	doc, err := publictheme.Parse(revision.DocumentJSON)
	if err != nil {
		return publicThemeEnvelope{}, err
	}
	doc.Revision = int(revision.Revision)
	return publicThemeEnvelope{Document: doc, Revision: int(revision.Revision), PublishedAt: revision.PublishedAt}, nil
}

func (s *Server) handlePublicTheme(w http.ResponseWriter, r *http.Request) {
	theme, err := s.publishedTheme(r.Context())
	if err != nil {
		s.logger.Error("public_theme_load_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "theme_error", "读取前台主题失败")
		return
	}
	writeJSON(w, http.StatusOK, theme)
}

func (s *Server) handleAdminThemeDraft(w http.ResponseWriter, r *http.Request) {
	published, err := s.publishedTheme(r.Context())
	if err != nil {
		s.logger.Error("published_theme_load_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "theme_error", "读取已发布主题失败")
		return
	}
	draft, err := s.store.GetPublicThemeRevision(r.Context(), "draft")
	if errors.Is(err, sqlite.ErrPublicThemeNotFound) {
		writeJSON(w, http.StatusOK, adminThemeEnvelope{
			Document: published.Document, Revision: published.Revision,
			PublishedRevision: published.Revision, PublishedAt: published.PublishedAt, HasDraft: false,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取主题草稿失败")
		return
	}
	doc, err := publictheme.Parse(draft.DocumentJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "theme_error", "主题草稿已损坏")
		return
	}
	doc.Revision = int(draft.Revision)
	updatedAt := draft.CreatedAt
	writeJSON(w, http.StatusOK, adminThemeEnvelope{
		Document: doc, Revision: int(draft.Revision), UpdatedAt: &updatedAt,
		PublishedRevision: published.Revision, PublishedAt: published.PublishedAt, HasDraft: true,
	})
}

type updateThemeDraftRequest struct {
	Document publictheme.Document `json:"document"`
}

func (s *Server) handleUpdateThemeDraft(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input updateThemeDraftRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 160<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "主题配置格式无效")
		return
	}
	doc, err := publictheme.Normalize(input.Document)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_theme", err.Error())
		return
	}
	doc.Revision = 0
	raw, err := json.Marshal(doc)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_theme", "主题配置编码失败")
		return
	}
	now := time.Now().UTC()
	draft, err := s.store.SavePublicThemeDraft(r.Context(), raw, now)
	if err != nil {
		s.logger.Error("save_theme_draft_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "主题草稿保存失败")
		return
	}
	doc.Revision = int(draft.Revision)
	published, err := s.publishedTheme(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "theme_error", "读取已发布主题失败")
		return
	}
	updatedAt := draft.CreatedAt
	writeJSON(w, http.StatusOK, adminThemeEnvelope{
		Document: doc, Revision: int(draft.Revision), UpdatedAt: &updatedAt,
		PublishedRevision: published.Revision, PublishedAt: published.PublishedAt, HasDraft: true,
	})
}

func (s *Server) handlePublishThemeDraft(w http.ResponseWriter, r *http.Request) {
	published, err := s.store.PublishPublicThemeDraft(r.Context(), time.Now().UTC())
	if errors.Is(err, sqlite.ErrPublicThemeNotFound) {
		writeError(w, http.StatusConflict, "no_theme_draft", "没有可发布的主题草稿")
		return
	}
	if err != nil {
		s.logger.Error("publish_theme_draft_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "主题发布失败")
		return
	}
	doc, err := publictheme.Parse(published.DocumentJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "theme_error", "已发布主题配置无效")
		return
	}
	doc.Revision = int(published.Revision)
	s.logger.Info("public_theme_published", "revision", published.Revision)
	writeJSON(w, http.StatusOK, publicThemeEnvelope{Document: doc, Revision: int(published.Revision), PublishedAt: published.PublishedAt})
}

func (s *Server) handleThemeRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := s.store.ListPublicThemeRevisions(r.Context(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取主题版本失败")
		return
	}
	items := make([]publicThemeRevisionItem, 0, len(revisions))
	for _, revision := range revisions {
		items = append(items, publicThemeRevisionItem{
			Revision: int(revision.Revision), Status: revision.Status,
			CreatedAt: revision.CreatedAt, PublishedAt: revision.PublishedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
