import { AdminSelectField } from "@/components/admin-select-field"
import { Button } from "@/components/ui/button"
import { useAuth } from "@/context/auth"
import { useAcceleratorInventory } from "@/hooks/use-accelerator-inventory"
import { acceleratorDeviceName } from "@/lib/accelerator-public"
import { acceleratorAlertInputs } from "@/lib/alert-accelerator"
import { useAcceleratorText } from "@/pages/accelerator-text"
import type { EvaluationFieldsProps } from "./evaluation-rule-fields"
export function EvaluationAcceleratorScope({ draft, sources, onChange }: EvaluationFieldsProps) {
 const t = useAcceleratorText(), { principal } = useAuth()
 const nodeID = draft.scope.node_ids?.[0] ?? "", deviceID = draft.scope.device_id || "*"
 const source = useAcceleratorInventory(nodeID, `${principal?.user.id}/${principal?.session.id}`)
 const inputs = acceleratorAlertInputs(draft.metric)
 const options = [{ value: "*", label: t("anyGpu") }, ...(source.data?.devices ?? []).filter(device => inputs.every(metric => device.capabilities.includes(metric))).map(device => ({ value: device.device_id, label: `${acceleratorDeviceName(device)}${device.inactive_at ? t("removedSuffix") : ""}` }))]
 if (!options.some(option => option.value === deviceID)) options.push({ value: deviceID, label: t("deviceUnavailable") })
 return <div className="space-y-3"><AdminSelectField id="evaluation-gpu-node" label={t("node")} value={nodeID || "choose"} options={[{ value: "choose", label: t("chooseNode") }, ...sources.nodes.map(node => ({ value: node.id, label: node.alias || node.name }))]} onValueChange={node => onChange({ ...draft, scope: { kind: "nodes", node_ids: node === "choose" ? [] : [node], device_id: "*" } })} />
  <AdminSelectField id="evaluation-gpu-device" label={t("device")} value={deviceID} options={options} disabled={!nodeID || source.loading} onValueChange={device_id => onChange({ ...draft, scope: { kind: "nodes", node_ids: [nodeID], device_id } })} />
  <p className="text-xs text-muted-foreground">{t("deviceScopeNote")}</p>
  {source.error && <p role="alert" className="text-xs text-destructive">{source.error}<Button variant="link" onClick={source.reload}>{t("retry")}</Button></p>}
 </div>
}
