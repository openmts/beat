export type EnrollmentApprovalMode = "manual" | "automatic"
export type EnrollmentRequestStatus = "pending" | "approved" | "rejected" | "expired" | "claimed"

export interface EnrollmentGrant {
  id: string
  name: string
  secret_prefix: string
  mode: EnrollmentApprovalMode
  enabled: boolean
  expires_at: string
  revoked_at?: string
  default_group_id: string
  default_group_name: string
  node_name_prefix: string
  tags: string[]
  remarks: string
  public: boolean
  ssh_port: number
  source_ip_as_host: boolean
  source_cidrs: string[]
  hostname_pattern: string
  claim_recovery: boolean
  maximum_enrollments: number
  consumed_enrollments: number
  created_at: string
  updated_at: string
}

/** 创建与覆盖共用同一份策略载荷；秘密由服务端生成，客户端永不提交。 */
export interface EnrollmentGrantWrite {
  name: string
  mode: EnrollmentApprovalMode
  enabled: boolean
  expires_at: string
  default_group_id: string
  node_name_prefix: string
  tags: string[]
  remarks: string
  public: boolean
  ssh_port: number
  source_ip_as_host: boolean
  source_cidrs: string[]
  hostname_pattern: string
  claim_recovery: boolean
  maximum_enrollments: number
}

export interface EnrollmentGrantSecret {
  grant: EnrollmentGrant
  secret: string
}

export interface EnrollmentRequest {
  id: string
  grant_id: string
  grant_name: string
  status: EnrollmentRequestStatus
  node_name: string
  reserved_node_id?: string
  hostname: string
  advertised_host: string
  os: string
  architecture: string
  agent_version: string
  virtualization: string
  ssh_port: number
  source_ip: string
  installation_nonce: string
  first_request_at: string
  last_request_at: string
  expires_at: string
  reason?: string
  approved_by?: string
  rejected_by?: string
  claimed_at?: string
}

export interface EnrollmentRequestPage {
  requests: EnrollmentRequest[]
  next_cursor: string
}

export interface EnrollmentApprovalWrite {
  node_name: string
  group_id: string
  tags: string[]
  remarks: string
  public: boolean
  host: string
  ssh_port: number
}

export interface EnrollmentRequestQuery {
  status?: EnrollmentRequestStatus | ""
  grant_id?: string
  cursor?: string
  limit?: number
}
