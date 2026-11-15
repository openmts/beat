CREATE TABLE IF NOT EXISTS public_theme_revisions (
    revision INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'archived')),
    document_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    published_at INTEGER
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_public_theme_single_draft
    ON public_theme_revisions(status) WHERE status = 'draft';
CREATE UNIQUE INDEX IF NOT EXISTS idx_public_theme_single_published
    ON public_theme_revisions(status) WHERE status = 'published';
