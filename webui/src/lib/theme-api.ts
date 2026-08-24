import { api } from "@/lib/api-client"
import type {
  ActiveThemeDescriptor,
  ThemeCatalog,
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
  const form = new FormData()
  form.append("archive", archive)
  return (await api.post<ThemePackage>("/admin/themes/upload", form)).data
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

export async function deleteTheme(id: string): Promise<void> {
  await api.delete(`/admin/themes/${id}`)
}

export async function listThemeMarkets(): Promise<ThemeMarketSource[]> {
  return (await api.get<ThemeMarketSource[]>("/admin/theme-markets")).data
}

export async function createThemeMarket(payload: {
  name: string
  catalog_url: string
  public_key: string
  key_fingerprint: string
  enabled: boolean
  sort_order: number
}): Promise<ThemeMarketSource> {
  return (await api.post<ThemeMarketSource>("/admin/theme-markets", payload)).data
}

export async function updateThemeMarket(
  id: string,
  payload: {
    name: string
    catalog_url: string
    public_key: string
    key_fingerprint: string
    enabled: boolean
    sort_order: number
  },
): Promise<ThemeMarketSource> {
  return (await api.put<ThemeMarketSource>(`/admin/theme-markets/${id}`, payload)).data
}

export async function deleteThemeMarket(id: string): Promise<void> {
  await api.delete(`/admin/theme-markets/${id}`)
}

export async function refreshThemeCatalog(id: string): Promise<ThemeCatalog | { cached: boolean }> {
  return (await api.get<ThemeCatalog | { cached: boolean }>(`/admin/theme-markets/${id}/catalog`)).data
}

export async function installMarketTheme(sourceId: string, themeId: string): Promise<ThemePackage> {
  return (await api.post<ThemePackage>(
    `/admin/theme-markets/${sourceId}/themes/${themeId}/install`,
  )).data
}