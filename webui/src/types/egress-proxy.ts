export interface EgressProxySettings {
  enabled: boolean;
  proxy_url: string;
  username: string;
  has_password: boolean;
  revision: number;
  updated_at: string;
}

export interface EgressProxyUpdate {
  enabled: boolean;
  proxy_url: string;
  username: string;
  password?: string;
  clear_password: boolean;
  expected_revision: number;
}

export interface EgressProxyProbeResult {
  checked_at: string;
  latency_ms: number;
}
