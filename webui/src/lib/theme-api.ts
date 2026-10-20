import { api } from "@/lib/api-client"
import type {
  ActiveThemeDescriptor,
  ThemeCatalog,
  ThemeCatalogEntry,
  ThemeDeletion,
  ThemeMarketInput,
  ThemeMarketSource,
  ThemePackage,
} from "@/types/theme"

export async function getActiveTheme(): Promise<ActiveThemeDescriptor> {
  return (await api.get<ActiveThemeDescriptor>("/themes/active")).data
}

export async function listAdminThemes(): Promise<ThemePackage[]> {
  return (await api.get<ThemePackage[]>("/admin/themes")).data
}

export async function uploadTheme(archive: Blob): Promise<ThemePackage> {
  return (await api.post<ThemePackage>("/admin/themes/upload", archive, {
    headers: { "Content-Type": "application/zip" },
  })).data
}

export async function exportTheme(id: string): Promise<Blob> {
  return (await api.get<Blob>(`/admin/themes/${id}/export`, { responseType: "blob" })).data
}

export async function activateTheme(id: string): Promise<ThemePackage> {
  return (await api.post<ThemePackage>(`/admin/themes/${id}/activate`)).data
}

export async function rollbackTheme(): Promise<{ rollback_target: string }> {
  return (await api.post<{ rollback_target: string }>("/admin/themes/rollback")).data
}

export async function deleteTheme(id: string): Promise<ThemeDeletion | undefined> {
  const response = await api.delete<ThemeDeletion>(`/admin/themes/${id}`)
  return response.status === 202 ? response.data : undefined
}

export async function listThemeMarkets(): Promise<ThemeMarketSource[]> {
  return (await api.get<ThemeMarketSource[]>("/admin/theme-markets")).data
}

export async function createThemeMarket(payload: ThemeMarketInput): Promise<ThemeMarketSource> {
  return (await api.post<ThemeMarketSource>("/admin/theme-markets", payload)).data
}

export async function updateThemeMarket(
  id: string,
  payload: ThemeMarketInput,
): Promise<ThemeMarketSource> {
  return (await api.put<ThemeMarketSource>(`/admin/theme-markets/${id}`, payload)).data
}

export async function deleteThemeMarket(id: string): Promise<void> {
  await api.delete(`/admin/theme-markets/${id}`)
}

export async function refreshThemeCatalog(id: string): Promise<ThemeCatalog> {
  return (await api.get<ThemeCatalog>(`/admin/theme-markets/${id}/catalog`)).data
}

export async function installMarketTheme(sourceId: string, entry: Pick<ThemeCatalogEntry, "theme_id" | "version" | "package_sha256">): Promise<ThemePackage> {
  return (await api.post<ThemePackage>(
    `/admin/theme-markets/${sourceId}/themes/${entry.theme_id}/install`,
    { version: entry.version, package_sha256: entry.package_sha256 },
  )).data
}

export async function previewTheme(id: string): Promise<ActiveThemeDescriptor> {
  return (await api.get<ActiveThemeDescriptor>(`/admin/themes/${id}/preview`)).data
}

export async function configureTheme(id: string, configuration: import("@/types/theme-definition").ThemeConfiguration): Promise<ActiveThemeDescriptor> {
  return (await api.put<ActiveThemeDescriptor>(`/admin/themes/${id}/configuration`, configuration)).data
}

export async function useBuiltInTheme(): Promise<void> {
  await api.post("/admin/themes/built-in")
}
