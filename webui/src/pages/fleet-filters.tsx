import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useFleetText } from "@/pages/fleet-text"
export function FleetMultiFilter({ label, options, value, change }: { label: string; options: Array<{ value: string; label: string }>; value: string[]; change: (value: string[]) => void }) {
 const [open, setOpen] = useState(false), [search, setSearch] = useState(""), t = useFleetText()
 const names = options.filter(option => value.includes(option.value)).map(option => option.label).join(", ")
 const matching = options.filter(option => option.label.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
 return <><Button size="sm" variant="outline" className="max-w-64 justify-start" title={names || label} onClick={() => setOpen(true)}><span className="truncate">{label}: {names || t("all")}</span></Button>
  <Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle>{label}</DialogTitle></DialogHeader><Input aria-label={`${label} ${t("search")}`} value={search} onChange={event => setSearch(event.target.value)} /><p className="text-xs text-muted-foreground">{value.length}/16 · {matching.length}</p>
   <div className="max-h-72 space-y-2 overflow-auto">{matching.slice(0, 100).map(option => <label key={option.value} className="flex items-center gap-2 text-sm"><Checkbox checked={value.includes(option.value)} disabled={!value.includes(option.value) && value.length >= 16} onCheckedChange={checked => change(checked ? [...value, option.value] : value.filter(id => id !== option.value))} />{option.label}</label>)}</div>
   {matching.length > 100 && <p className="text-sm text-muted-foreground">{t("filterLimit")}</p>}<DialogFooter><Button variant="outline" onClick={() => change([])}>{t("clear")}</Button><Button onClick={() => setOpen(false)}>{t("done")}</Button></DialogFooter>
  </DialogContent></Dialog>
 </>
}
