import { api } from "@/lib/api-client";
import type {
  EgressProxySettings,
  EgressProxyProbeResult,
  EgressProxyUpdate,
} from "@/types/egress-proxy";

export async function getEgressProxy(
  signal?: AbortSignal,
): Promise<EgressProxySettings> {
  const response = await api.get<EgressProxySettings>(
    "/admin/settings/egress-proxy",
    { signal },
  );
  return response.data;
}

export async function updateEgressProxy(
  input: EgressProxyUpdate,
): Promise<EgressProxySettings> {
  const response = await api.put<EgressProxySettings>(
    "/admin/settings/egress-proxy",
    input,
  );
  return response.data;
}

export async function testEgressProxy(): Promise<EgressProxyProbeResult> {
  const response = await api.post<EgressProxyProbeResult>(
    "/admin/settings/egress-proxy/test",
  );
  return response.data;
}
