package model

import "time"

// 主题运行时类型与来源。
const (
	ThemeRuntimeDeclarative = "declarative"
	ThemeRuntimeSandboxed   = "sandboxed"
	ThemeSourceUpload       = "upload"
	ThemeSourceMarket       = "market"
	ThemeStateInstalled     = "installed"
	ThemeStateQuarantined   = "quarantined"
	ThemeStateDeleted       = "deleted"
	ThemeActivationReason   = "activate"
	ThemeRollbackReason     = "rollback"
	ThemeFallbackReason     = "fallback"
)

// ThemeManifest 是主题包根目录 beat-theme.json 的严格 schema。
type ThemeManifest struct {
	SchemaVersion      int                 `json:"schema_version"`
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	Description        string              `json:"description"`
	Version            string              `json:"version"`
	Author             string              `json:"author"`
	License            string              `json:"license"`
	Runtime            string              `json:"runtime"`
	MinimumBeatVersion string              `json:"minimum_beat_version"`
	MaximumBeatVersion string              `json:"maximum_beat_version,omitempty"`
	Preview            string              `json:"preview"`
	Homepage           string              `json:"homepage,omitempty"`
	ReleaseNotes       string              `json:"release_notes,omitempty"`
	SupportedLocales   []string            `json:"supported_locales,omitempty"`
	Files              []ThemeManifestFile `json:"files"`
}

// ThemeManifestFile 是清单文件清单项。
type ThemeManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ThemePackage 是 theme_packages 行。
type ThemePackage struct {
	ID                 string    `json:"id"`
	ThemeID            string    `json:"theme_id"`
	Name               string    `json:"name"`
	Version            string    `json:"version"`
	Author             string    `json:"author"`
	Description        string    `json:"description"`
	License            string    `json:"license"`
	Runtime            string    `json:"runtime"`
	ManifestSchema     int       `json:"manifest_schema"`
	MinimumBeatVersion string    `json:"minimum_beat_version"`
	MaximumBeatVersion string    `json:"maximum_beat_version,omitempty"`
	ArchiveDigest      string    `json:"archive_digest"`
	ArchiveBytes       int64     `json:"archive_bytes"`
	ContentBytes       int64     `json:"content_bytes"`
	ManifestJSON       string    `json:"-"`
	ConfigurationJSON  string    `json:"configuration,omitempty"`
	SourceType         string    `json:"source_type"`
	SourceID           string    `json:"source_id,omitempty"`
	State              string    `json:"state"`
	ValidatedAt        time.Time `json:"validated_at"`
	CreatorID          string    `json:"creator_id"`
	LastErrorCategory  string    `json:"last_error_category,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	IsActive           bool      `json:"is_active,omitempty"`
	HealthState        string    `json:"health_state,omitempty"`
}

// ThemeActivation 是 theme_activations 行。
type ThemeActivation struct {
	ID                  string     `json:"id"`
	PackageID           string     `json:"package_id"`
	PreviousPackageID   string     `json:"previous_package_id,omitempty"`
	ActorID             string     `json:"actor_id"`
	Reason              string     `json:"reason"`
	ActivatedAt         time.Time  `json:"activated_at"`
	DeactivatedAt       *time.Time `json:"deactivated_at,omitempty"`
	HealthState         string     `json:"health_state"`
	LastFailureCategory string     `json:"last_failure_category,omitempty"`
}

// ThemeMarketSource 是 theme_market_sources 行。
type ThemeMarketSource struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	CatalogURL        string     `json:"catalog_url"`
	Enabled           bool       `json:"enabled"`
	SortOrder         int        `json:"sort_order"`
	PublicKey         string     `json:"-"`
	KeyFingerprint    string     `json:"key_fingerprint"`
	CacheETag         string     `json:"-"`
	CacheLastModified string     `json:"-"`
	FetchedAt         *time.Time `json:"fetched_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CatalogDigest     string     `json:"catalog_digest,omitempty"`
	LastStatus        string     `json:"last_status"`
	LastErrorCategory string     `json:"last_error_category,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// ThemeCatalog 是市场目录 schema 版本 1。
type ThemeCatalog struct {
	SchemaVersion int                 `json:"schema_version"`
	SourceID      string              `json:"source_id"`
	Themes        []ThemeCatalogEntry `json:"themes"`
}

// ThemeCatalogEntry 是目录中的主题条目。
type ThemeCatalogEntry struct {
	ThemeID        string `json:"theme_id"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	Author         string `json:"author"`
	Description    string `json:"description"`
	Homepage       string `json:"homepage,omitempty"`
	Runtime        string `json:"runtime"`
	PackageURL     string `json:"package_url"`
	PackageSHA256  string `json:"package_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	MinimumBeat    string `json:"minimum_beat_version"`
	MaximumBeat    string `json:"maximum_beat_version,omitempty"`
	PreviewURL     string `json:"preview_url,omitempty"`
	ReleaseNotes   string `json:"release_notes,omitempty"`
}

// ActiveThemeDescriptor 是公开的活动主题描述（无文件系统细节）。
type ActiveThemeDescriptor struct {
	PackageID     string `json:"package_id,omitempty"`
	ThemeID       string `json:"theme_id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Runtime       string `json:"runtime"`
	BuiltIn       bool   `json:"built_in"`
	Protocol      int    `json:"protocol"`
	Configuration string `json:"configuration,omitempty"`
}
