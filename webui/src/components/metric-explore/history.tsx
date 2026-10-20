import { useMemo, useState, type KeyboardEvent } from "react"
import { Link2Icon, StarIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { filterExploreHistory, nextExploreHistoryIndex, sortExploreHistory, type ExploreHistoryEntry, type ExploreHistoryOrder } from "@/lib/explore-history"

/** 排序选项与 `explore.history_sort_<value>` 文案键一一对应。 */
const sortOptions: ExploreHistoryOrder[] = ["newest", "oldest"]

export interface ExploreHistoryProps {
  entries: ExploreHistoryEntry[]
  onRestore: (entry: ExploreHistoryEntry) => void
  onToggleStar: (entry: ExploreHistoryEntry, starred: boolean) => void
  onDelete: (searches: string[]) => void
  onClear: () => void
  close: () => void
}

/** 查询历史对话框：可搜索、可用键盘高亮打开、可多选删除；星标条目在整体清理后保留。 */
export function ExploreHistoryDialog(props: ExploreHistoryProps) {
  const { locale, t } = useLocale()
  const [needle, setNeedle] = useState("")
  const [selected, setSelected] = useState<string[]>([])
  const [active, setActive] = useState(-1)
  const [confirming, setConfirming] = useState(false)
  const [status, setStatus] = useState("")
  const [order, setOrder] = useState<ExploreHistoryOrder>("newest")
  const [starredOnly, setStarredOnly] = useState(false)
  const entries = useMemo(() => sortExploreHistory(filterExploreHistory(props.entries, needle, { starredOnly }), order), [props.entries, needle, order, starredOnly])
  const activeIndex = active >= entries.length ? entries.length - 1 : active
  const clearable = props.entries.filter((entry) => !entry.starred).length
  const allSelected = entries.length > 0 && entries.every((entry) => selected.includes(entry.search))

  const toggleSelected = (search: string, checked: boolean) =>
    setSelected((current) => checked ? [...current, search] : current.filter((value) => value !== search))
  function toggleAll(checked: boolean) { setSelected(checked ? entries.map((entry) => entry.search) : []) }
  function open(entry: ExploreHistoryEntry) { props.onRestore(entry); props.close() }
  function removeSelected() {
    const removable = selected.filter((search) => props.entries.some((entry) => entry.search === search))
    props.onDelete(removable)
    setSelected([])
    setStatus(t("explore.history_deleted", { count: String(removable.length) }))
  }
  function clear() {
    setStatus(t("explore.history_cleared", { count: String(clearable) }))
    setConfirming(false)
    setSelected([])
    props.onClear()
  }
  function navigate(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp" || event.key === "Home" || event.key === "End") {
      event.preventDefault()
      setActive(nextExploreHistoryIndex(entries.length, activeIndex, event.key))
      return
    }
    if (event.key === "Enter" && entries[activeIndex]) { event.preventDefault(); open(entries[activeIndex]!) }
  }

  return <Dialog open onOpenChange={(openDialog) => { if (!openDialog) props.close() }}>
    <DialogContent className="sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>{t("explore.history")}</DialogTitle>
        <DialogDescription>{t("explore.history_description")}</DialogDescription>
      </DialogHeader>
      <div className="grid gap-2">
        <Label htmlFor="explore-history-search">{t("explore.history_search")}</Label>
        <Input id="explore-history-search" aria-controls="explore-history-list" value={needle}
          onChange={(event) => { setNeedle(event.target.value); setActive(-1) }} onKeyDown={navigate} />
        <div className="flex flex-wrap items-end gap-3">
          <div className="grid gap-2">
            <Label htmlFor="explore-history-sort">{t("explore.history_sort")}</Label>
            <Select items={sortOptions.map((value) => ({ value, label: t(`explore.history_sort_${value}`) }))} value={order}
              onValueChange={(value) => { setOrder((value ?? "newest") as ExploreHistoryOrder); setActive(-1) }}>
              <SelectTrigger id="explore-history-sort" className="w-full sm:w-40"><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>
                {sortOptions.map((value) => <SelectItem key={value} value={value}>{t(`explore.history_sort_${value}`)}</SelectItem>)}
              </SelectGroup></SelectContent>
            </Select>
          </div>
          <label className="flex items-center gap-2 pb-1 text-sm">
            <Checkbox checked={starredOnly} onCheckedChange={(checked) => { setStarredOnly(checked === true); setActive(-1) }} />
            {t("explore.history_only_starred")}
          </label>
        </div>
        <p className="text-xs text-muted-foreground">{t("explore.history_keyboard_hint")}</p>
      </div>
      {entries.length === 0
        ? <p className="py-8 text-center text-sm text-muted-foreground">{t("explore.history_empty")}</p>
        : <>
          <label className="flex items-center gap-2 text-sm"><Checkbox checked={allSelected} onCheckedChange={(checked) => toggleAll(checked === true)} />{t("explore.history_select_all")}</label>
          <ul id="explore-history-list" aria-label={t("explore.history")} className="max-h-80 divide-y overflow-auto rounded-md border">
            {entries.map((entry, index) => <HistoryRow key={entry.search} entry={entry} locale={locale} active={index === activeIndex} selected={selected.includes(entry.search)}
              select={(checked) => toggleSelected(entry.search, checked)} restore={() => open(entry)} star={(starred) => props.onToggleStar(entry, starred)} />)}
          </ul>
        </>}
      {status && <p role="status" className="text-xs text-muted-foreground">{status}</p>}
      <DialogFooter className="sm:justify-between">
        <Button size="sm" variant="outline" disabled={selected.length === 0} onClick={removeSelected}>
          {t("explore.history_delete_selected", { count: String(selected.length) })}
        </Button>
        {confirming
          ? <div className="flex gap-2">
            <Button size="sm" variant="destructive" onClick={clear}>{t("explore.history_clear")}</Button>
            <Button size="sm" variant="outline" onClick={() => setConfirming(false)}>{t("workspace.cancel")}</Button>
          </div>
          : <Button size="sm" variant="outline" disabled={clearable === 0} onClick={() => setConfirming(true)}>
            {t("explore.history_clear")}
          </Button>}
      </DialogFooter>
    </DialogContent>
  </Dialog>
}

function HistoryRow(props: { entry: ExploreHistoryEntry; locale: string; active: boolean; selected: boolean; select: (checked: boolean) => void; restore: () => void; star: (starred: boolean) => void }) {
  const { t } = useLocale()
  const { entry } = props
  return <li data-active={props.active ? "true" : undefined} className={`flex items-start gap-3 p-3 ${props.active ? "bg-muted/60 ring-1 ring-inset ring-ring" : ""}`}>
    <Checkbox className="mt-0.5" checked={props.selected} aria-label={`${t("explore.history_select")} · ${entry.summary || t("explore.no_queries")}`} onCheckedChange={(checked) => props.select(checked === true)} />
    <div className="min-w-0 flex-1">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="secondary">{entry.query_type === "instant" ? t("explore.query_type_instant") : t("explore.query_type_range")}</Badge>
        <span className="truncate text-sm font-medium">{entry.summary || t("explore.no_queries")}</span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">{formatDateTime(props.locale, new Date(entry.ran_at))}</p>
    </div>
    <Button variant="ghost" size="icon" aria-label={entry.starred ? t("explore.history_unstar") : t("explore.history_star")}
      aria-pressed={entry.starred} onClick={() => props.star(!entry.starred)}>
      <StarIcon data-icon fill={entry.starred ? "currentColor" : "none"} />
    </Button>
    <Button variant="outline" size="sm" onClick={props.restore}>
      <Link2Icon data-icon="inline-start" />{t("explore.history_open")}
    </Button>
  </li>
}
