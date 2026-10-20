import { api } from "@/lib/api-client"

const enrollmentRequestTimeout = 15_000

export interface AgentEnrollmentCommand {
  id: string
  token: string
  server_url: string
  expires_at: string
  command: string
}

export async function createAgentEnrollmentToken(payload: {
  server_url: string
  allow_http: boolean
}): Promise<AgentEnrollmentCommand> {
  return (await api.post<AgentEnrollmentCommand>("/admin/agent-enrollment-tokens", payload, {
    timeout: enrollmentRequestTimeout,
  })).data
}
