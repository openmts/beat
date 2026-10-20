import { acceleratorMetricKeys, isAcceleratorMetric } from "@/lib/accelerator-metrics"
import type { AcceleratorPolicy, AcceleratorPolicyWrite } from "@/types/accelerator"
export function acceleratorPolicyWrite(policy: AcceleratorPolicy): AcceleratorPolicyWrite {
 if (!Number.isInteger(policy.sample_interval_seconds) || policy.sample_interval_seconds < 5 || policy.sample_interval_seconds > 3600) throw new Error("GPU sample interval must be between 5 and 3600 seconds")
 if (!Number.isInteger(policy.maximum_devices) || policy.maximum_devices < 1 || policy.maximum_devices > 32) throw new Error("GPU device limit must be between 1 and 32")
 if (policy.metrics.length > acceleratorMetricKeys.length || new Set(policy.metrics).size !== policy.metrics.length || policy.metrics.some(metric => !isAcceleratorMetric(metric))) throw new Error("Invalid GPU metric allowlist")
 return { enabled: policy.enabled, sample_interval_seconds: policy.sample_interval_seconds, maximum_devices: policy.maximum_devices, metrics: [...policy.metrics], expected_revision: policy.revision }
}
