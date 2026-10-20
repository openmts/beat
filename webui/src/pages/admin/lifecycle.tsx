import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { useAuth } from "@/context/auth"
import { useNodes } from "@/hooks/use-api"
import * as api from "@/lib/lifecycle-api"
import type { GeoIPSettings, LifecycleSettings, NodeLocation, NodeLifecycle, LifecycleEvent } from "@/types/node-lifecycle"
import { NodeLocationEditor } from "./node-location-editor"
import { NodeLifecycleEditor } from "./node-lifecycle-editor"
import { GeoIPSettingsEditor } from "./geoip-settings-editor"
import { LifecycleSettingsEditor } from "./lifecycle-settings-editor"
import { ChoiceField } from "./lifecycle-fields"
export default function LifecyclePage() {
 const { t } = useLocale(), { principal } = useAuth(), nodes = useNodes()
 const [id, setID] = useState(""), [generation, setGeneration] = useState(0)
 const owner = principal?.user.role === "owner", identity = `${principal?.user.id}/${principal?.session.id}`
 const refresh = () => setGeneration(current => current + 1)
 return <main className="space-y-6"><header><h1 className="text-2xl font-semibold">{t("lifecycle.country_location_and_lifecycle")}</h1><p className="text-sm text-muted-foreground">{t("lifecycle.manage_country_location_expiry_dates_and_prices")}</p></header>
  {owner && <GlobalSettings key={`${identity}/${generation}`} saved={refresh} />}
  {nodes.error && <p role="alert">{nodes.error}</p>}<ChoiceField label={t("lifecycle.node")} value={id} onChange={setID}><option value="">{t("lifecycle.choose_a_node")}</option>{(nodes.data ?? []).map(node => <option key={node.id} value={node.id}>{node.alias || node.name}</option>)}</ChoiceField>
  {id && <NodeSettings key={`${identity}/${id}/${generation}`} id={id} saved={refresh} />}
 </main>
}
function GlobalSettings({ saved }: { saved: () => void }) {
 const { t } = useLocale()
 const [data, setData] = useState<{ geo: GeoIPSettings; lifecycle: LifecycleSettings } | null>(null), [error, setError] = useState(""), [retry, setRetry] = useState(0)
 useEffect(() => {
  const controller = new AbortController(); setError(""); setData(null)
  Promise.all([api.getGeoIPSettings(controller.signal), api.getLifecycleSettings(controller.signal)]).then(([geo, lifecycle]) => { if (!controller.signal.aborted) setData({ geo, lifecycle }) }).catch((failure: unknown) => { if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : "Load failed") })
  return () => controller.abort()
 }, [retry])
 if (error) return <div role="alert">{error}<Button variant="outline" onClick={() => setRetry(v => v + 1)}>{t("lifecycle.retry")}</Button></div>
 if (!data) return <p role="status">{t("lifecycle.loading_defaults")}</p>
 return <div className="grid items-start gap-6 xl:grid-cols-2"><Card><CardContent><GeoIPSettingsEditor value={data.geo} saved={saved} /></CardContent></Card><Card><CardContent><LifecycleSettingsEditor value={data.lifecycle} saved={saved} /></CardContent></Card></div>
}
function NodeSettings({ id, saved }: { id: string; saved: () => void }) {
 const { t } = useLocale()
 const [data, setData] = useState<{ location: NodeLocation; lifecycle: NodeLifecycle; events: LifecycleEvent[] } | null>(null), [error, setError] = useState(""), [retry, setRetry] = useState(0)
 useEffect(() => {
  const controller = new AbortController(); setError(""); setData(null)
  Promise.all([api.getLocation(id, controller.signal), api.getLifecycle(id, controller.signal), api.getLifecycleEvents(id, controller.signal)]).then(([location, lifecycle, events]) => { if (!controller.signal.aborted) setData({ location, lifecycle, events }) }).catch((failure: unknown) => { if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : "Load failed") })
  return () => controller.abort()
 }, [id, retry])
 if (error) return <div role="alert">{error}<Button variant="outline" onClick={() => setRetry(v => v + 1)}>{t("lifecycle.retry")}</Button></div>
 if (!data) return <p role="status">{t("lifecycle.loading_node_settings")}</p>
 return <><div className="grid items-start gap-6 xl:grid-cols-2"><Card><CardContent><NodeLocationEditor value={data.location} writable saved={saved} /></CardContent></Card><Card><CardContent><NodeLifecycleEditor value={data.lifecycle} writable saved={saved} /></CardContent></Card></div>
  <section className="space-y-3"><h2 className="font-semibold">{t("lifecycle.lifecycle_history")}</h2>{data.events.length === 0 && <p className="text-sm text-muted-foreground">{t("lifecycle.no_events")}</p>}<ol className="space-y-2">{data.events.map(event => <li key={event.id} className="rounded-md border p-3 text-sm"><time>{event.created_at}</time> · {event.kind} · {event.old_expires_on || "—"} → {event.new_expires_on || "—"}<p className="text-muted-foreground">{event.actor} · {event.routing_status} · {t("lifecycle.skipped_intervals")}: {event.skipped_intervals}</p></li>)}</ol></section></>
}
