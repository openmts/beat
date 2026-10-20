import { useState } from "react"
import { ChevronDownIcon, LayoutGridIcon, SlidersHorizontalIcon, TableIcon } from "lucide-react"
import { AdminSelectField } from "@/components/admin-select-field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FleetMultiFilter } from "@/pages/fleet-filters"
import { useFleetText } from "@/pages/fleet-text"
import { countryName } from "@/lib/fleet-selectors"
import { defaultFleetControls, type FleetControls } from "@/lib/fleet-state"
import type { Group, Node } from "@/types"
import { useLocale } from "@/context/locale"

interface FleetToolbarProps {
  value: FleetControls
  change: (patch: Partial<FleetControls>) => void
  nodes: Node[]
  groups: Group[]
  count: number
}

function advancedFilterCount(value: FleetControls): number {
  return [
    Boolean(value.status),
    Boolean(value.freshness),
    Boolean(value.country),
    Boolean(value.expiry),
    value.sort !== defaultFleetControls.sort,
    value.direction !== defaultFleetControls.direction,
    value.offline !== defaultFleetControls.offline,
  ].filter(Boolean).length
}

export function FleetToolbar({ value, change, nodes, groups, count }: FleetToolbarProps) {
  const [expanded, setExpanded] = useState(false)
  const t = useFleetText(), { locale } = useLocale()
  const tags = [...new Set(nodes.flatMap(node => node.tags ?? []))].sort(), countries = [...new Set(nodes.map(node => node.location?.country_code).filter((code): code is string => Boolean(code)))].sort()
  const labels = (keys: string[]) => keys.map(key => ({ value: key || "*", label: key === "offline" ? t("offlineStatus") : t(key || "all") }))
  const field = (key: "status" | "freshness" | "expiry" | "sort" | "direction" | "offline", choices: string[]) => <AdminSelectField id={`fleet-${key}`} label={t(key)} options={labels(choices)} value={value[key] || "*"} onValueChange={next => change({ [key]: next === "*" ? "" : next })} />
  const activeAdvanced = advancedFilterCount(value)
  return <section aria-label={t("search")} className="border-y py-3">
    <div className="flex flex-col gap-2 xl:flex-row xl:items-center">
      <div className="flex min-w-0 flex-1 flex-wrap gap-2">
        <Input aria-label={t("search")} placeholder={t("search")} maxLength={128} className="min-w-40 flex-1" value={value.search} onChange={event => change({ search: event.target.value })} />
        {value.search && <Button size="sm" variant="outline" onClick={() => change({ search: "" })}>{t("clear")}</Button>}
        <FleetMultiFilter label={t("groups")} options={groups.map(group => ({ value: group.id, label: group.name }))} value={value.groups} change={groups => change({ groups })} />
        <FleetMultiFilter label={t("tags")} options={tags.map(tag => ({ value: tag, label: tag }))} value={value.tags} change={tags => change({ tags })} />
        <Button size="sm" variant={expanded ? "secondary" : "outline"} aria-controls="fleet-advanced-filters" aria-expanded={expanded} aria-label={`${t(expanded ? "hideFilters" : "moreFilters")}${activeAdvanced > 0 ? `, ${t("activeFilters", { count: activeAdvanced })}` : ""}`} onClick={() => setExpanded(current => !current)}>
          <SlidersHorizontalIcon />
          {t(expanded ? "hideFilters" : "moreFilters")}
          {activeAdvanced > 0 && <span aria-hidden className="rounded-full bg-primary/10 px-1.5 text-xs text-primary">{activeAdvanced}</span>}
          <ChevronDownIcon className={expanded ? "rotate-180 transition-transform" : "transition-transform"} />
        </Button>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2 xl:justify-end">
        <p aria-live="polite" className="text-xs text-muted-foreground">{t("results")}: <span className="font-medium text-foreground tabular-nums">{count}/{nodes.length}</span></p>
        <div className="flex gap-2"><Button size="sm" variant="outline" aria-pressed={value.layout === "cards"} title={t("cards")} onClick={() => change({ layout: "cards" })}><LayoutGridIcon />{t("cards")}</Button><Button size="sm" variant="outline" aria-pressed={value.layout === "table"} title={t("table")} onClick={() => change({ layout: "table" })}><TableIcon />{t("table")}</Button></div>
      </div>
    </div>
    {expanded && <div id="fleet-advanced-filters" className="grid grid-cols-2 gap-3 border-t pt-3 md:grid-cols-4 xl:grid-cols-7">
      {field("status", ["", "online", "offline"])}{field("freshness", ["", "fresh", "delayed", "stale", "unavailable"])}{field("sort", ["order", "name", "status", "cpu", "memory", "disk", "net_sent", "net_recv", ...(nodes.some(node => node.lifecycle?.status) ? ["expiry"] : [])])}{field("direction", ["asc", "desc"])}{field("offline", ["first", "keep", "last"])}
      {countries.length > 0 && <AdminSelectField id="fleet-country" label={t("country")} options={[{ value: "*", label: t("all") }, ...countries.map(code => ({ value: code, label: countryName(code, locale) }))]} value={value.country || "*"} onValueChange={country => change({ country: country === "*" ? "" : country })} />}
      {nodes.some(node => node.lifecycle?.status) && field("expiry", ["", "expired", "today", "7", "30", "untracked"])}
    </div>}
  </section>
}
