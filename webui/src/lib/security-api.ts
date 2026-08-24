import { api } from "@/lib/api-client"
import type {
  AdminPrincipal,
  AdminRole,
  AdminSession,
  AdminUser,
  AuditPage,
  AuthState,
  ExternalIdentity,
  OIDCProvider,
  OIDCProviderPublic,
  TOTPSetup,
} from "@/types/security"

export async function getAuthState(): Promise<AuthState> {
  return (await api.get<AuthState>("/auth/state")).data
}

export async function bootstrapAdmin(payload: {
  bootstrap_token: string
  username: string
  display_name: string
  password: string
}): Promise<AdminPrincipal> {
  return (await api.post<AdminPrincipal>("/auth/bootstrap", payload)).data
}

export async function loginAdmin(payload: {
  username: string
  password: string
  totp_code?: string
}): Promise<AdminPrincipal> {
  return (await api.post<AdminPrincipal>("/auth/login", payload)).data
}

export async function logoutAdmin(): Promise<void> {
  await api.post("/auth/logout")
}

export async function getAdminSession(): Promise<AdminPrincipal> {
  return (await api.get<AdminPrincipal>("/auth/session")).data
}

export async function reauthenticateAdmin(password: string, totpCode?: string): Promise<AdminSession> {
  return (await api.post<AdminSession>("/auth/reauthenticate", {
    password,
    totp_code: totpCode ?? "",
  })).data
}

export async function listAdminUsers(): Promise<AdminUser[]> {
  return (await api.get<AdminUser[]>("/admin/users")).data
}

export async function createAdminUser(payload: {
  username: string
  display_name: string
  role: AdminRole
  password: string
}): Promise<AdminUser> {
  return (await api.post<AdminUser>("/admin/users", payload)).data
}

export async function updateAdminUser(
  id: string,
  payload: { display_name: string; role: AdminRole; enabled: boolean },
): Promise<void> {
  await api.put(`/admin/users/${id}`, payload)
}

export async function deleteAdminUser(id: string): Promise<void> {
  await api.delete(`/admin/users/${id}`)
}

export async function changeAdminPassword(payload: {
  current_password: string
  new_password: string
  totp_code?: string
}): Promise<void> {
  await api.put("/admin/users/me/password", payload)
}

export async function beginTOTP(): Promise<TOTPSetup> {
  return (await api.post<TOTPSetup>("/admin/users/me/totp", { code: "" })).data
}

export async function enableTOTP(code: string): Promise<void> {
  await api.post("/admin/users/me/totp", { code })
}

export async function disableTOTP(): Promise<void> {
  await api.delete("/admin/users/me/totp")
}

export async function listAdminSessions(): Promise<AdminSession[]> {
  return (await api.get<AdminSession[]>("/admin/sessions")).data
}

export async function revokeAdminSession(id: string): Promise<void> {
  await api.delete(`/admin/sessions/${id}`)
}

export async function revokeOtherAdminSessions(): Promise<number> {
  return (await api.delete<{ revoked: number }>("/admin/sessions/others")).data.revoked
}

export async function listAuditEvents(offset = 0): Promise<AuditPage> {
  return (await api.get<AuditPage>("/admin/audit-events", { params: { limit: 50, offset } })).data
}

export async function listPublicOIDCProviders(): Promise<OIDCProviderPublic[]> {
  return (await api.get<OIDCProviderPublic[]>("/auth/oidc/providers")).data
}

export async function beginOIDCLogin(providerId: string, returnPath: string): Promise<string> {
  return (await api.post<{ authorization_url: string }>(`/auth/oidc/providers/${providerId}/begin`, {
    return_path: returnPath,
  })).data.authorization_url
}

export async function listMyExternalIdentities(): Promise<ExternalIdentity[]> {
  return (await api.get<ExternalIdentity[]>("/admin/oidc/identities/me")).data
}

export async function beginOIDCLink(providerId: string, returnPath: string): Promise<string> {
  return (await api.post<{ authorization_url: string }>(`/admin/oidc/providers/${providerId}/link`, {
    return_path: returnPath,
  })).data.authorization_url
}

export async function unlinkExternalIdentity(id: string): Promise<void> {
  await api.delete(`/admin/oidc/identities/${id}`)
}

export async function listAdminOIDCProviders(): Promise<OIDCProvider[]> {
  return (await api.get<OIDCProvider[]>("/admin/oidc/providers")).data
}

export async function createOIDCProvider(payload: {
  slug: string
  display_name: string
  issuer_url: string
  client_id: string
  client_secret: string
  scopes: string[]
  enabled: boolean
  sort_order: number
}): Promise<OIDCProvider> {
  return (await api.post<OIDCProvider>("/admin/oidc/providers", payload)).data
}

export async function updateOIDCProvider(
  id: string,
  payload: {
    slug: string
    display_name: string
    issuer_url: string
    client_id: string
    client_secret?: string
    scopes: string[]
    enabled: boolean
    sort_order: number
  },
): Promise<OIDCProvider> {
  return (await api.put<OIDCProvider>(`/admin/oidc/providers/${id}`, payload)).data
}

export async function deleteOIDCProvider(id: string): Promise<void> {
  await api.delete(`/admin/oidc/providers/${id}`)
}
