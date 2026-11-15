export type NodeStatus = "online" | "warning" | "offline";

export interface Metrics {
  timestamp: string;
  cpuUsage: number;
  cpuUser: number;
  cpuSystem: number;
  cpuIowait: number;
  memoryUsedBytes: number;
  memoryTotalBytes: number;
  memoryUsage: number;
  swapUsedBytes: number;
  diskUsedBytes: number;
  diskTotalBytes: number;
  diskUsage: number;
  networkRxBps: number;
  networkTxBps: number;
  load1: number;
  load5: number;
  load15: number;
  processes: number;
  tcpConnections: number;
  uptimeSeconds: number;
}

export interface Node {
  id: string;
  agentId: string;
  hostname: string;
  groupId: string;
  groupName: string;
  groupColor: string;
  address: string;
  os: string;
  kernel: string;
  arch: string;
  agentVersion: string;
  status: NodeStatus;
  labels: Record<string, string>;
  cpuCores: number;
  cpuModel: string;
  totalMemoryBytes: number;
  totalDiskBytes: number;
  collectorIntervalSeconds: number;
  firstSeen: string;
  lastSeen: string;
  metrics: Metrics;
}

export interface PublicNode {
  id: string;
  hostname: string;
  groupId: string;
  groupName: string;
  groupColor: string;
  address: string;
  os: string;
  kernel: string;
  arch: string;
  status: NodeStatus;
  labels: Record<string, string>;
  cpuCores: number;
  cpuModel: string;
  totalMemoryBytes: number;
  totalDiskBytes: number;
  collectorIntervalSeconds: number;
  firstSeen: string;
  lastSeen: string;
  metrics: Metrics;
}

export interface PublicNodeGroup {
  id: string;
  name: string;
  description: string;
  color: string;
  nodeCount: number;
  onlineCount: number;
  warningCount: number;
  offlineCount: number;
}

export interface PublicGroupsResponse {
  items: PublicNodeGroup[];
  total: number;
  ungroupedCount: number;
}

export type PublicThemeMode = "system" | "light" | "dark";
export type PublicContentWidth = "standard" | "wide" | "full";
export type PublicRadius = "sm" | "md" | "lg";
export type PublicSurface = "solid" | "gradient" | "mesh";
export type PublicDensity = "compact" | "comfortable" | "spacious";
export type PublicBlockHeight = "auto" | "sm" | "md" | "lg" | "tall" | "viewport";
export type PublicBlockPadding = "none" | "sm" | "md" | "lg";
export type PublicBlockGap = "sm" | "md" | "lg";

export interface PublicThemeAppearance {
  mode: PublicThemeMode;
  primary: string;
  accent: string;
  contentWidth: PublicContentWidth;
  radius: PublicRadius;
  surface: PublicSurface;
  density: PublicDensity;
}

export interface PublicThemeLayout {
  minHeight: PublicBlockHeight;
  padding: PublicBlockPadding;
  columns: number;
  gap: PublicBlockGap;
}

export interface PublicThemeBlock {
  id: string;
  type: string;
  enabled: boolean;
  layout: PublicThemeLayout;
  props: Record<string, unknown>;
}

export interface PublicThemePage {
  blocks: PublicThemeBlock[];
}

export interface PublicThemeDocument {
  schemaVersion: number;
  revision: number;
  presetId?: string;
  appearance: PublicThemeAppearance;
  home: PublicThemePage;
  nodeDetail: PublicThemePage;
}

export interface PublicThemePreset {
  id: string;
  name: string;
  description: string;
  isDefault: boolean;
  document: PublicThemeDocument;
}

export interface ThemeCatalogResponse {
  items: PublicThemePreset[];
}

export interface PublicThemeResponse {
  document: PublicThemeDocument;
  revision: number;
  publishedAt?: string | null;
}

export interface PublicBootstrapResponse {
  site: SiteSettings;
  theme: PublicThemeDocument;
  revision: number;
  publishedAt?: string | null;
  counts: Record<string, number>;
  generatedAt: string;
}

export interface AdminThemeDraftResponse extends PublicThemeResponse {
  updatedAt?: string | null;
  publishedRevision: number;
  hasDraft: boolean;
}

export interface ThemeRevision {
  revision: number;
  status: "draft" | "published" | "archived";
  createdAt: string;
  publishedAt?: string | null;
}

export interface ThemeRevisionsResponse {
  items: ThemeRevision[];
}

export interface OverviewSummary {
  nodeCount: number;
  onlineCount: number;
  offlineCount: number;
  warningCount: number;
  avgCpu: number;
  avgMemory: number;
  avgDisk: number;
  networkRx: number;
  networkTx: number;
}

export interface FleetPoint {
  timestamp: string;
  cpuUsage: number;
  memoryUsage: number;
  diskUsage: number;
  networkRx: number;
  networkTx: number;
}

export interface OverviewResponse {
  generatedAt: string;
  range: string;
  summary: OverviewSummary;
  fleet: FleetPoint[];
  nodes: Node[];
  topCpu: Node[];
}

export interface NodesResponse {
  items: Node[];
  total: number;
  counts: Record<string, number>;
}

export interface NodeUpdate {
  hostname: string;
  labels: Record<string, string>;
  groupId: string;
}

export interface NodeGroup {
  id: string;
  name: string;
  description: string;
  color: string;
  nodeCount: number;
  onlineCount: number;
  warningCount: number;
  offlineCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface GroupsResponse {
  items: NodeGroup[];
  total: number;
  ungroupedCount: number;
}

export interface NodeGroupInput {
  name: string;
  description: string;
  color: string;
}

export interface UninstallNodeResponse {
  deleted: boolean;
  nodeId: string;
  hostname: string;
  agentId: string;
  task: {
    id: string;
    agentId: string;
    nodeId: string;
    kind: "uninstall_agent";
    status: string;
    createdAt: string;
  };
  metricsDeleted: boolean;
}

export interface Agent {
  id: string;
  nodeId: string;
  nodeName: string;
  version: string;
  status: NodeStatus;
  intervalSeconds: number;
  lastSeen: string;
  createdAt: string;
}

export interface AgentArchitecture {
  id: "amd64" | "arm64";
  label: string;
  available: boolean;
  downloadUrl: string;
  checksumSha256?: string;
  downloadCommand: string;
}

export interface AgentsResponse {
  items: Agent[];
  total: number;
  tokenTtlSeconds: number;
  architectures: AgentArchitecture[];
}

export interface AgentUpgradeArtifact {
  arch: AgentArchitecture["id"];
  version: string;
  size: number;
  sha256: string;
  available: boolean;
}

export type AgentUpgradeItemStatus = "queued" | "delivered" | "installing" | "restarting" | "succeeded" | "failed";

export interface AgentUpgradeItem {
  id: string;
  batchId: string;
  agentId: string;
  nodeId: string;
  nodeName: string;
  arch: string;
  fromVersion: string;
  toVersion: string;
  artifactSha256: string;
  artifactSize: number;
  status: AgentUpgradeItemStatus;
  message: string;
  attempt: number;
  createdAt: string;
  updatedAt: string;
  deliveredAt?: string;
  completedAt?: string;
}

export interface AgentUpgradeBatch {
  id: string;
  targetVersion: string;
  status: "running" | "completed" | "partial";
  total: number;
  queued: number;
  delivered: number;
  succeeded: number;
  failed: number;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  items: AgentUpgradeItem[];
}

export interface AgentUpgradeOverview {
  targetVersion: string;
  artifacts: AgentUpgradeArtifact[];
  totalNodes: number;
  upToDate: number;
  upgradable: number;
  online: number;
  offline: number;
  unsupported: number;
  activeBatch?: AgentUpgradeBatch;
  recentBatches: AgentUpgradeBatch[];
}

export interface AgentUpgradeCreateResponse {
  batch: AgentUpgradeBatch;
  skipped: number;
}

export interface AgentEnrollment {
  token: string;
  name: string;
  labels: string;
  createdAt: string;
  expiresAt: string;
  ttlSeconds: number;
  accessCommand: string;
}

export interface SeriesPoint {
  timestamp: string;
  cpuUsage: number;
  cpuUser: number;
  cpuSystem: number;
  memoryUsage: number;
  diskUsage: number;
  networkRxBps: number;
  networkTxBps: number;
  load1: number;
  load5: number;
  load15: number;
  processes: number;
  tcpConnections: number;
}

export interface NodeMetricsResponse {
  nodeId: string;
  from: string;
  to: string;
  stepSeconds: number;
  points: SeriesPoint[];
}

export interface NetworkProbeTargetSeries {
  carrier: Carrier;
  label: string;
  lossPercent: Array<number | null>;
  latencyAvgMs: Array<number | null>;
  latencyP95Ms: Array<number | null>;
  latencyJitterMs: Array<number | null>;
}

export interface NetworkProbeTaskSeries {
  taskId: string;
  taskName: string;
  lastRunAt: string;
  timestamps: string[];
  targets: NetworkProbeTargetSeries[];
}

export interface NodeNetworkProbesResponse {
  nodeId: string;
  from: string;
  to: string;
  stepSeconds: number;
  tasks: NetworkProbeTaskSeries[];
}

export interface SettingsResponse {
  server: {
    version: string;
    startedAt: string;
    uptimeSeconds: number;
    listen: string;
    demoMode: boolean;
  };
  storage: {
    sqlitePath: string;
    dataDir: string;
    mtsPath: string;
    retention: string;
    retentionDays: number;
    walSync: boolean;
    mts: { driver: string; path: string; healthy: boolean; ready: boolean; measurements: number };
  };
  collector: { intervalSeconds: number; offlineAfterSeconds: number; defaultRange: string; enrollmentTokenTTLSeconds: number };
  configuration: {
    site: SiteSettings;
    storage: StorageSettings;
  };
  updatedAt: string | null;
  restartRequired: boolean;
}

export interface SiteSettings {
  name: string;
  subtitle: string;
  publicUrl: string;
  timezone: string;
}

export interface StorageSettings {
  mtsDataPath: string;
  retentionDays: number;
  walSync: boolean;
}

export interface SystemSettingsInput {
  site: SiteSettings;
  storage: StorageSettings;
}

export interface MetaResponse {
  version: string;
  serverTime: string;
  demoMode: boolean;
  nodeCount: number;
  site: SiteSettings;
  agentArchitectures: AgentArchitecture[];
  enrollmentTokenTtlSeconds: number;
}

export interface AuthStatus {
  authenticated: boolean;
  username?: string;
  csrfToken?: string;
  expiresAt?: string;
}

export interface CredentialsUpdateInput {
  currentPassword: string;
  username: string;
  newPassword: string;
}

export interface CredentialsUpdateResponse extends AuthStatus {
  username: string;
  updatedAt: string;
  expiresAt: string;
}

export type TaskSource = "custom";
export type TaskKind = "shell_script" | "three_network_probe";
export type TaskRunStatus = "delivered" | "success" | "failed" | "timeout";
export type Carrier = "telecom" | "unicom" | "mobile";
export type AddressFamily = "auto" | "ipv4" | "ipv6";

export interface ThreeNetworkTarget {
  carrier: Carrier;
  label: string;
  host: string;
  port: number;
  protocol: "tcp";
  addressFamily: AddressFamily;
}

export interface ProbeSettings {
  count: number;
  intervalMs: number;
  timeoutMs: number;
}

export interface ThreeNetworkConfig {
  targets: ThreeNetworkTarget[];
  probe: ProbeSettings;
}

export type MetricExtractorType = "json_path" | "key_value" | "regex";

export interface TaskMetricExtractor {
  type: MetricExtractorType;
  expression: string;
}

export interface TaskMetricDefinition {
  key: string;
  name: string;
  extractor: TaskMetricExtractor;
  unit: string;
}

export interface ShellMetricConfig {
  metricSchema: {
    enabled: boolean;
    measurement: string;
    metrics: TaskMetricDefinition[];
  };
}

export interface TaskDefinition {
  id: string;
  slug: string;
  name: string;
  description: string;
  source: TaskSource;
  kind: TaskKind;
  script: string;
  config: ThreeNetworkConfig | ShellMetricConfig | Record<string, never>;
  intervalSeconds: number;
  timeoutSeconds: number;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
  lastRunAt?: string;
  lastStatus?: TaskRunStatus;
  runCount: number;
  successCount: number;
  failureCount: number;
}

export interface TaskSummary {
  total: number;
  enabled: number;
  runs: number;
  failures: number;
}

export interface TasksResponse {
  items: TaskDefinition[];
  total: number;
  summary: TaskSummary;
}

export interface TaskRun {
  id: string;
  taskId: string;
  taskName: string;
  taskSource: TaskSource;
  agentId: string;
  nodeId: string;
  nodeName: string;
  script?: string;
  timeoutSeconds: number;
  status: TaskRunStatus;
  scheduledAt: string;
  deliveredAt?: string;
  completedAt?: string;
  exitCode: number;
  durationMs: number;
  output: string;
  error?: string;
  createdAt: string;
}

export interface TaskRunsResponse {
  items: TaskRun[];
  total: number;
}

export interface TaskInput {
  name: string;
  description: string;
  kind: TaskKind;
  script?: string;
  config?: ThreeNetworkConfig | ShellMetricConfig | Record<string, never>;
  intervalSeconds: number;
  timeoutSeconds: number;
  enabled: boolean;
}

export type TaskUpdateInput = Partial<TaskInput>;

export interface NetworkProbeTargetResult {
  carrier: Carrier;
  label: string;
  host: string;
  port: number;
  protocol: "tcp";
  family: AddressFamily;
  resolvedIp: string;
  resolveMs: number;
  loss: {
    available: boolean;
    sent: number;
    received: number;
    lost: number;
    failed: number;
    percent: number | null;
  };
  latency: {
    available: boolean;
    sampleCount: number;
    minMs: number | null;
    avgMs: number | null;
    maxMs: number | null;
    p50Ms: number | null;
    p95Ms: number | null;
    jitterMs: number | null;
  };
  samples: Array<{
    index: number;
    outcome: "success" | "lost" | "failed";
    rttMs: number | null;
    error?: string;
  }>;
  status: "ok" | "degraded" | "failed";
  errors?: string[];
}

export interface NetworkProbeResult {
  kind: "three_network_probe";
  startedAt: string;
  durationMs: number;
  targets: NetworkProbeTargetResult[];
}

