import { useLocale } from "@/context/locale"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import type { AgentRolloutPolicy } from "@/types/agent-rollouts"
import { fields } from "./agent-rollout-config"
export function AgentRolloutPolicyFields({ value, change }: { value: AgentRolloutPolicy; change: (value: AgentRolloutPolicy) => void }) {
 const { t } = useLocale()
 return <fieldset className="space-y-4"><legend className="font-semibold">{t("agentRollout.policy_legend")}</legend><div className="grid gap-4 sm:grid-cols-2">{fields.map(([key, label, min, max]) => <div key={key} className="space-y-2"><Label htmlFor={key}>{t(label)}</Label><Input id={key} type="number" min={min} max={max} step={1} required value={Number.isNaN(value[key]) ? "" : value[key]} onChange={event => change({ ...value, [key]: event.target.value === "" ? NaN : Number(event.target.value) })} /></div>)}</div>
  <label className="flex gap-2 text-sm"><Checkbox checked={value.allow_preview} onCheckedChange={checked => change({ ...value, allow_preview: Boolean(checked) })} />{t("agentRollout.allow_preview")}</label>
  {([['window_start', 'agentRollout.maintenance_start'], ['window_end', 'agentRollout.maintenance_end']] as const).map(([key, label]) => <div key={key} className="space-y-2"><Label htmlFor={key}>{t("agentRollout.local_time", { label: t(label) })}</Label><Input id={key} type="datetime-local" required value={value[key]} onChange={event => change({ ...value, [key]: event.target.value })} /></div>)}
 </fieldset>
}
