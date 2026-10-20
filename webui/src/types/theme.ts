import type { ThemeConfigurationSchema, ThemeDefinition } from "@/types/theme-definition"

export interface ActiveThemeDescriptor {
  package_id?: string
  theme_id: string
  name: string
  version: string
  runtime: "declarative" | "sandboxed"
  built_in: boolean
  protocol: number
  configuration?: string
  /** 主题自声明的配置架构；省略表示这份包只允许站点改四个显示字段。 */
  configuration_schema?: ThemeConfigurationSchema
  sandbox_entry?: string
  archive_digest?: string
  preview_url?: string
  declarative?: ThemeDefinition
}

export interface ThemePackage {
  id: string
  theme_id: string
  name: string
  version: string
  author: string
  description: string
  license: string
  runtime: "declarative" | "sandboxed"
  manifest_schema: number
  minimum_beat_version: string
  maximum_beat_version?: string
  archive_digest: string
  archive_bytes: number
  content_bytes: number
  configuration?: string
  source_type: "upload" | "market"
  source_id?: string
  state: "installed" | "quarantined" | "deleted"
  validated_at: string
  creator_id: string
  last_error_category?: string
  created_at: string
  updated_at: string
  is_active?: boolean
  health_state?: string
  preview_url?: string
}

export interface ThemeMarketSource {
  id: string
  name: string
  catalog_url: string
  enabled: boolean
  sort_order: number
  key_fingerprint: string
  fetched_at?: string
  expires_at?: string
  catalog_digest?: string
  last_status: string
  last_error_category?: string
  created_at: string
  updated_at: string
}

export interface ThemeCatalogEntry {
  theme_id: string
  name: string
  version: string
  author: string
  description: string
  homepage?: string
  runtime: string
  package_url: string
  package_sha256: string
  manifest_sha256: string
  minimum_beat_version: string
  maximum_beat_version?: string
  preview_url?: string
  release_notes?: string
  /** 目标包声明的站点可配置项（与预览描述符的 configuration_schema 同形状）；缺省表示历史四字段。 */
  configuration?: ThemeConfigurationSchema
}

export interface ThemeCatalog {
  schema_version: number
  source_id: string
  expires_at: string
  catalog_digest: string
  cached: boolean
  stale: boolean
  error_category?: string
  fetched_at: string
  cache_expires_at: string
  themes: ThemeCatalogEntry[]
}

export interface ThemeMarketInput {
  name: string
  catalog_url: string
  public_key: string
  key_fingerprint: string
  enabled: boolean
  sort_order: number
}

export interface ThemeDeletion {
  package_id: string
  state: "deleted"
  cleanup_pending: boolean
}
