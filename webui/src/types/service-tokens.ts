export const serviceScopes = [
  "nodes:read",
  "nodes:write-presentation",
  "metrics:read",
  "network-tasks:read",
  "network-tasks:run-test",
  "alerts:read",
  "notification-test:send",
  "traffic-reports:read",
  "traffic-reports:run-test",
  "backups:create",
  "backups:read-metadata",
  "backups:download",
  "audit:read",
  "health:read",
] as const

export type ServiceScope = (typeof serviceScopes)[number]

export const serviceResourceKinds = ["node", "group", "channel"] as const

export type ServiceResourceKind = (typeof serviceResourceKinds)[number]

export interface ServiceTokenResource {
  kind: ServiceResourceKind
  id: string
}

export interface ServiceToken {
  id: string
  name: string
  purpose: string
  prefix: string
  visibility: "public" | "private"
  source_cidrs: string[]
  scopes: ServiceScope[]
  resources?: ServiceTokenResource[]
  expires_at: string
  revoked_at?: string
  revoked_reason?: string
  superseded_at?: string
  rotation_overlap_seconds: number
  rotated_from?: string
  revision: number
  created_by: string
  created_at: string
  last_used_at?: string
}

export interface ServiceTokenCredential {
  token: string
  principal: ServiceToken
}

export interface ServiceTokenInput {
  name: string
  purpose?: string
  scopes: ServiceScope[]
  resources?: ServiceTokenResource[]
  visibility: "public" | "private"
  source_cidrs?: string[]
  expires_at: string
  extended?: boolean
}

export interface ServiceTokenRotationInput extends ServiceTokenInput {
  overlap_seconds: number
}
