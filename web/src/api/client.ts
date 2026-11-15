import type {
  AgentEnrollment,
  AgentUpgradeBatch,
  AgentUpgradeCreateResponse,
  AgentUpgradeOverview,
  AdminThemeDraftResponse,
  AgentsResponse,
  AuthStatus,
  CredentialsUpdateInput,
  CredentialsUpdateResponse,
  TaskInput,
  GroupsResponse,
  MetaResponse,
  Node,
  NodeGroup,
  NodeGroupInput,
  NodeMetricsResponse,
  NodeNetworkProbesResponse,
  PublicBootstrapResponse,
  PublicGroupsResponse,
  PublicNode,
  PublicThemeDocument,
  ThemeCatalogResponse,
  PublicThemeResponse,
  NodeUpdate,
  NodesResponse,
  OverviewResponse,
  SettingsResponse,
  SystemSettingsInput,
  TaskDefinition,
  TaskRun,
  TaskRunsResponse,
  TasksResponse,
  TaskUpdateInput,
  ThemeRevisionsResponse,
  UninstallNodeResponse,
} from "./types";

export class ApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

let csrfToken = "";

function isMutation(method: string | undefined): boolean {
  return Boolean(method && !["GET", "HEAD", "OPTIONS"].includes(method.toUpperCase()));
}

async function refreshCSRFToken(): Promise<void> {
  try {
    const response = await fetch("/api/v1/auth/me", { credentials: "same-origin", headers: { Accept: "application/json" } });
    if (!response.ok) return;
    const payload = await response.json() as { csrfToken?: string };
    if (payload.csrfToken) csrfToken = payload.csrfToken;
  } catch {
    // The mutation itself will surface a useful network/server error.
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  if (isMutation(init?.method) && !csrfToken && path !== "/api/v1/auth/login") {
    await refreshCSRFToken();
  }
  const headers: Record<string, string> = { "Content-Type": "application/json", ...(init?.headers as Record<string, string> | undefined) };
  if (csrfToken && isMutation(init?.method)) headers["X-Beat-CSRF"] = csrfToken;
  let response: Response;
  try {
    response = await fetch(path, { credentials: "same-origin", ...init, headers });
  } catch {
    throw new ApiError(0, "network_error", "网络连接失败，请检查 Beat 服务是否运行");
  }
  if (!response.ok) {
    let code = "request_failed";
    let message = `请求失败 (${response.status})`;
    try {
      const body = await response.json() as { code?: string; message?: string };
      code = body.code ?? code;
      message = body.message ?? message;
    } catch {
      // Keep the HTTP fallback message when the body is not JSON.
    }
    if (response.status === 401 && typeof window !== "undefined") {
      window.dispatchEvent(new Event("beat:unauthorized"));
    }
    throw new ApiError(response.status, code, message);
  }
  const payload = await response.json() as T & { csrfToken?: string };
  if (payload.csrfToken) csrfToken = payload.csrfToken;
  if (path === "/api/v1/auth/logout") csrfToken = "";
  return payload;
}

export const api = {
  meta: () => request<MetaResponse>("/api/v1/meta"),
  authMe: () => request<AuthStatus>("/api/v1/auth/me"),
  login: (username: string, password: string) => request<AuthStatus>("/api/v1/auth/login", { method: "POST", body: JSON.stringify({ username, password }) }),
  logout: () => request<AuthStatus>("/api/v1/auth/logout", { method: "POST" }),
  updateCredentials: (input: CredentialsUpdateInput) => request<CredentialsUpdateResponse>("/api/v1/auth/credentials", { method: "PUT", body: JSON.stringify(input) }),
  tasks: () => request<TasksResponse>("/api/v1/tasks"),
  createTask: (input: TaskInput) => request<TaskDefinition>("/api/v1/tasks", { method: "POST", body: JSON.stringify(input) }),
  updateTask: (id: string, input: TaskUpdateInput) => request<TaskDefinition>(`/api/v1/tasks/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(input) }),
  deleteTask: (id: string) => request<{ deleted: boolean; id: string }>(`/api/v1/tasks/${encodeURIComponent(id)}`, { method: "DELETE" }),
  taskRuns: (taskId = "", limit = 50) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (taskId) params.set("taskId", taskId);
    return request<TaskRunsResponse>(`/api/v1/task-runs?${params.toString()}`);
  },
  taskRun: (id: string) => request<TaskRun>(`/api/v1/task-runs/${encodeURIComponent(id)}`),
  overview: (range: string) => request<OverviewResponse>(`/api/v1/overview?range=${encodeURIComponent(range)}`),
  nodes: (q = "", status = "all") => {
    const params = new URLSearchParams();
    if (q.trim()) params.set("q", q.trim());
    if (status !== "all") params.set("status", status);
    const suffix = params.size ? `?${params.toString()}` : "";
    return request<NodesResponse>(`/api/v1/nodes${suffix}`);
  },
  node: (id: string) => request<Node>(`/api/v1/nodes/${encodeURIComponent(id)}`),
  updateNode: (id: string, update: NodeUpdate) => request<Node>(`/api/v1/nodes/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(update) }),
  uninstallNode: (id: string) => request<UninstallNodeResponse>(`/api/v1/nodes/${encodeURIComponent(id)}`, { method: "DELETE" }),
  nodeMetrics: (id: string, range: string) => request<NodeMetricsResponse>(`/api/v1/nodes/${encodeURIComponent(id)}/metrics?range=${encodeURIComponent(range)}`),
  nodeNetworkProbes: (id: string, range: string) => request<NodeNetworkProbesResponse>(`/api/v1/nodes/${encodeURIComponent(id)}/network-probes?range=${encodeURIComponent(range)}`),
  publicBootstrap: () => request<PublicBootstrapResponse>("/api/v1/public/bootstrap"),
  publicTheme: () => request<PublicThemeResponse>("/api/v1/public/theme"),
  publicNodes: (q = "", status = "all") => {
    const params = new URLSearchParams();
    if (q.trim()) params.set("q", q.trim());
    if (status !== "all") params.set("status", status);
    const suffix = params.size ? `?${params.toString()}` : "";
    return request<{ items: PublicNode[]; total: number; counts: Record<string, number> }>(`/api/v1/public/nodes${suffix}`);
  },
  publicNode: (id: string) => request<PublicNode>(`/api/v1/public/nodes/${encodeURIComponent(id)}`),
  publicNodeMetrics: (id: string, range: string) => request<NodeMetricsResponse>(`/api/v1/public/nodes/${encodeURIComponent(id)}/metrics?range=${encodeURIComponent(range)}`),
  publicNodeNetworkProbes: (id: string, range: string) => request<NodeNetworkProbesResponse>(`/api/v1/public/nodes/${encodeURIComponent(id)}/network-probes?range=${encodeURIComponent(range)}`),
  publicGroups: () => request<PublicGroupsResponse>("/api/v1/public/groups"),
  themeDraft: () => request<AdminThemeDraftResponse>("/api/v1/admin/theme/draft"),
  themeCatalog: () => request<ThemeCatalogResponse>("/api/v1/admin/theme/catalog"),
  saveThemeDraft: (document: PublicThemeDocument) => request<AdminThemeDraftResponse>("/api/v1/admin/theme/draft", { method: "PUT", body: JSON.stringify({ document }) }),
  publishTheme: () => request<PublicThemeResponse>("/api/v1/admin/theme/publish", { method: "POST" }),
  themeRevisions: () => request<ThemeRevisionsResponse>("/api/v1/admin/theme/revisions"),
  groups: () => request<GroupsResponse>("/api/v1/groups"),
  createGroup: (input: NodeGroupInput) => request<NodeGroup>("/api/v1/groups", { method: "POST", body: JSON.stringify(input) }),
  updateGroup: (id: string, input: NodeGroupInput) => request<NodeGroup>(`/api/v1/groups/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(input) }),
  deleteGroup: (id: string) => request<{ deleted: boolean; group: NodeGroup }>(`/api/v1/groups/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setGroupMembers: (id: string, nodeIds: string[]) => request<NodeGroup>(`/api/v1/groups/${encodeURIComponent(id)}/members`, { method: "PUT", body: JSON.stringify({ nodeIds }) }),
  agents: () => request<AgentsResponse>("/api/v1/agents"),
  createAgentToken: (name: string, labels: string) => request<AgentEnrollment>("/api/v1/agent-tokens", { method: "POST", body: JSON.stringify({ name, labels }) }),
  agentUpgradeOverview: () => request<AgentUpgradeOverview>("/api/v1/agent-upgrades/overview"),
  createAgentUpgrade: (includeOffline: boolean) => request<AgentUpgradeCreateResponse>("/api/v1/agent-upgrades", { method: "POST", body: JSON.stringify({ includeOffline }) }),
  agentUpgradeBatch: (id: string) => request<AgentUpgradeBatch>(`/api/v1/agent-upgrades/${encodeURIComponent(id)}`),
  retryAgentUpgrade: (id: string) => request<AgentUpgradeBatch>(`/api/v1/agent-upgrades/${encodeURIComponent(id)}/retry`, { method: "POST" }),
  settings: () => request<SettingsResponse>("/api/v1/settings"),
  updateSettings: (settings: SystemSettingsInput) => request<SettingsResponse>("/api/v1/settings", { method: "PUT", body: JSON.stringify({ settings }) }),
};

