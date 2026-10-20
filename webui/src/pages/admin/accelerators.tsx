import { evaluationTime } from "@/lib/alert-evaluation-format"
import { useState } from "react"
import { Link } from "react-router"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AcceleratorEnrollmentPolicy } from "./accelerator-enrollment-policy"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { AdminSelectField } from "@/components/admin-select-field"
import { useManagedNodes } from "@/hooks/use-api"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { useAcceleratorInventory } from "@/hooks/use-accelerator-inventory"
import { acceleratorDeviceName, acceleratorStale } from "@/lib/accelerator-public"
import { useAcceleratorText } from "@/pages/accelerator-text"
import { AcceleratorCurrent } from "@/components/accelerator-current"
import { AcceleratorPolicyEditor } from "./accelerator-policy-editor"
import { AcceleratorVisibilityToggle } from "./accelerator-visibility-toggle"
export default function AcceleratorsPage() {
 const nodes = useManagedNodes(), [nodeID, setNodeID] = useState(""), { principal } = useAuth(), t = useAcceleratorText(), { locale } = useLocale(), { settings } = useSiteSettings()
 const identity = `${principal?.user.id}/${principal?.session.id}`, source = useAcceleratorInventory(nodeID, identity), node = nodes.data?.find(value => value.id === nodeID)
 return <main className="space-y-6"><header><h1 className="text-2xl font-semibold">{t("title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t("adminDescription")}</p></header>
  <Tabs defaultValue="nodes"><TabsList><TabsTrigger value="nodes">{t("node")}</TabsTrigger>{principal?.user.role === "owner" && <TabsTrigger value="enrollment">{t("newNodeDefaults")}</TabsTrigger>}</TabsList><TabsContent value="nodes" className="mt-4 space-y-6">
  {nodes.error && <Alert variant="destructive"><AlertDescription>{nodes.error}</AlertDescription></Alert>}
  <AdminSelectField id="gpu-node" label={t("node")} value={nodeID || "choose"} options={[{ value: "choose", label: t("chooseNode") }, ...(nodes.data ?? []).map(value => ({ value: value.id, label: value.alias || value.name }))]} onValueChange={value => setNodeID(value === "choose" ? "" : value)} />
  {node && <p className="text-sm text-muted-foreground"><Badge variant="outline">{node.is_public ? t("publicNode") : t("privateNode")}</Badge> {t("privacy")} <Link className="underline" to="/admin/metrics">{t("metricVisibility")}</Link></p>}
  {source.loading && <Skeleton className="h-64" />}{source.error && <Alert variant="destructive"><AlertDescription>{source.error}<Button variant="outline" onClick={source.reload}>{t("retry")}</Button></AlertDescription></Alert>}
  {source.data && <div className="grid items-start gap-6 xl:grid-cols-[minmax(18rem,24rem)_1fr]"><div className="space-y-6"><Card><CardHeader><CardTitle>{t("policy")}</CardTitle></CardHeader><CardContent><AcceleratorPolicyEditor key={`${source.key}/${source.data.policy.revision}`} value={source.data.policy} scope={{ kind: "node", id: nodeID }} saved={source.reload} /></CardContent></Card><Card><CardContent className="pt-6"><AcceleratorVisibilityToggle nodeID={nodeID} visible={source.data.public_visible} globalVisible={settings.show_accelerators} saved={source.reload} /></CardContent></Card></div>
   <section className="space-y-4"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="font-semibold">{t("inventory")}</h2><Button variant="outline" onClick={source.reload}>{t("refresh")}</Button></div><div className="flex flex-wrap gap-2"><Badge variant={source.data.state === "ok" ? "default" : "outline"}>{source.data.state === "ok" ? t("collectionReady") : t(source.data.state)}</Badge>{source.data.error_code && <Badge variant="outline">{source.data.error_code}</Badge>}<time className="text-xs text-muted-foreground" dateTime={source.data.observed_at} title={source.data.observed_at}>{evaluationTime(source.data.observed_at, locale)}</time></div>
    {!source.data.devices.length && <p className="rounded-md border border-dashed p-8 text-center text-muted-foreground">{t("empty")}</p>}
    {source.data.devices.map(device => <Card key={device.device_id}><CardHeader><CardTitle className="truncate" title={acceleratorDeviceName(device)}>{acceleratorDeviceName(device)}</CardTitle><p className="text-xs text-muted-foreground">{device.vendor} · {device.driver_version || "—"} · {device.inactive_at ? t("inactive") : t(acceleratorStale(device) ? "stale" : "fresh")}</p></CardHeader><CardContent><AcceleratorCurrent device={device} /><p className="mt-3 text-xs text-muted-foreground">{t("observed")}: <time dateTime={device.last_seen_at} title={device.last_seen_at}>{evaluationTime(device.last_seen_at, locale)}</time></p>{device.error_codes?.length > 0 && <p role="status" className="text-xs text-muted-foreground">{device.error_codes.join(" · ")}</p>}</CardContent></Card>)}
   </section></div>}
 </TabsContent>{principal?.user.role === "owner" && <TabsContent value="enrollment" className="mt-4"><AcceleratorEnrollmentPolicy /></TabsContent>}</Tabs></main>
}
