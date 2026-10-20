import { useId, useState } from "react"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { useEvaluationText } from "@/pages/admin/evaluation-text"

interface Props { items: Array<{ id: string; name: string }>; value: string[]; limit: number; onChange: (ids: string[]) => void; disabled?: boolean }
export function EvaluationTargets({ items, value, limit, onChange, disabled }: Props) {
  const t = useEvaluationText(), prefix = useId()
  const [search, setSearch] = useState("")
  const available = new Set(items.map(item => item.id))
  const choices = [...items, ...value.filter(id => !available.has(id)).map(id => ({ id, name: `${id} (${t("unknown")})` }))]
  const matches = choices.filter(item => item.name.toLowerCase().includes(search.toLowerCase()))
  const selected = new Set(value)
  return <div className="space-y-2 rounded-md border p-3">
    <Input aria-label={t("search")} placeholder={t("search")} value={search} onChange={event => setSearch(event.target.value)} disabled={disabled} />
    <p className="text-xs text-muted-foreground">{t("selected")}: {value.length} / {limit}</p>
    <div className="max-h-40 space-y-2 overflow-y-auto">{matches.slice(0, 100).map(item => <label key={item.id} htmlFor={`${prefix}-${item.id}`} className="flex items-center gap-2 text-sm"><Checkbox id={`${prefix}-${item.id}`} checked={selected.has(item.id)} disabled={disabled || (!selected.has(item.id) && value.length >= limit)} onCheckedChange={checked => onChange(checked ? [...value, item.id] : value.filter(id => id !== item.id))} />{item.name}</label>)}</div>
    {matches.length > 100 && <p className="text-xs text-muted-foreground">{t("limited")}</p>}
  </div>
}
