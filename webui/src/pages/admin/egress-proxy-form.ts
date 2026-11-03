import type { EgressProxySettings } from "@/types/egress-proxy";

export interface EgressProxyFormState {
  enabled: boolean;
  proxy_url: string;
  username: string;
  password: string;
  clear_password: boolean;
}

export const emptyEgressProxyForm: EgressProxyFormState = {
  enabled: false,
  proxy_url: "",
  username: "",
  password: "",
  clear_password: false,
};

export function egressProxyFormFromSettings(
  settings: EgressProxySettings,
): EgressProxyFormState {
  return {
    enabled: settings.enabled,
    proxy_url: settings.proxy_url,
    username: settings.username,
    password: "",
    clear_password: false,
  };
}
