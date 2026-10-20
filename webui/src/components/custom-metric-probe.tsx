import { useEffect, useState } from "react"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Loader2Icon, PlayIcon } from "lucide-react"
import { useNodes } from "@/hooks/use-api"
import type { CustomMetric, CustomMetricProbe } from "@/types"

// 轮询间隔与次数上限：目标是让管理员在数十秒内看到结果，同时不把离线节点变成无限请求。
const probePollIntervalMS = 1500
const probePollAttempts = 20

interface CustomMetricProbeDialogProps {
  metric: CustomMetric | null
  onClose: () => void
}

function nodeLabel(nodes: { id: string; name: string; alias?: string }[] | null | undefined, nodeId: string): string {
  const node = nodes?.find((candidate) => candidate.id === nodeId)
  return node?.alias || node?.name || nodeId
}

/** CustomMetricProbeDialog 在绑定节点上跑一次指标命令，验证命令可用并回显原始输出。 */
export function CustomMetricProbeDialog({ metric, onClose }: CustomMetricProbeDialogProps) {
  const { t } = useLocale()
  const { data: nodes } = useNodes()
  const [boundNodes, setBoundNodes] = useState<string[]>([])
  const [nodeId, setNodeId] = useState("")
  const [loadingNodes, setLoadingNodes] = useState(false)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [probe, setProbe] = useState<CustomMetricProbe | null>(null)

  useEffect(() => {
    if (!metric) return
    let cancelled = false
    setLoadingNodes(true)
    setProbe(null)
    setError(null)
    api.listCustomMetricNodes(metric.id)
      .then((ids) => {
        if (cancelled) return
        setBoundNodes(ids)
        setNodeId((current) => (ids.includes(current) ? current : ids[0] ?? ""))
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : t("app.error"))
      })
      .finally(() => {
        if (!cancelled) setLoadingNodes(false)
      })
    return () => { cancelled = true }
  }, [metric, t])

  useEffect(() => {
    if (!metric || probe?.status !== "pending") return
    let cancelled = false
    let attempts = 0
    let timer: number | undefined
    const metricID = metric.id
    const taskID = probe.task_id
    const poll = async () => {
      attempts += 1
      try {
        const result = await api.getCustomMetricProbe(metricID, taskID)
        if (cancelled) return
        setProbe(result)
        if (result.status === "pending" && attempts >= probePollAttempts) {
          if (timer !== undefined) window.clearInterval(timer)
          setError(t("custom_metric.probe_timeout"))
        }
      } catch (err) {
        if (cancelled) return
        if (timer !== undefined) window.clearInterval(timer)
        setError(err instanceof Error ? err.message : t("app.error"))
      }
    }
    timer = window.setInterval(() => void poll(), probePollIntervalMS)
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearInterval(timer)
    }
  }, [metric, probe?.status, probe?.task_id, t])

  const runProbe = async () => {
    if (!metric || !nodeId) return
    setRunning(true)
    setError(null)
    setProbe(null)
    try {
      setProbe(await api.probeCustomMetric(metric.id, nodeId))
    } catch (err) {
      setError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setRunning(false)
    }
  }

  const nodeItems = boundNodes.map((id) => ({ label: nodeLabel(nodes, id), value: id }))

  return (
    <Dialog open={!!metric} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{metric ? `${metric.name} ${t("custom_metric.probe")}` : ""}</DialogTitle>
          <DialogDescription>{t("custom_metric.probe_description")}</DialogDescription>
        </DialogHeader>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {loadingNodes ? (
          <Skeleton className="h-10 w-full" />
        ) : boundNodes.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">{t("custom_metric.probe_unbound")}</p>
        ) : (
          <div className="flex flex-col gap-2">
            <Label>{t("custom_metric.probe_node")}</Label>
            <div className="flex gap-2">
              <Select items={nodeItems} value={nodeId} onValueChange={(value) => { if (value) setNodeId(value) }}>
                <SelectTrigger className="flex-1">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {nodeItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button onClick={() => void runProbe()} disabled={running || !nodeId}>
                {running ? <Loader2Icon data-icon="inline-start" className="animate-spin" /> : <PlayIcon data-icon="inline-start" />}
                {t("custom_metric.probe_run")}
              </Button>
            </div>
          </div>
        )}
        {probe && <ProbeResult probe={probe} />}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t("app.close")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function probeStatusVariant(status: CustomMetricProbe["status"]): "default" | "secondary" | "destructive" {
  if (status === "done") return "default"
  if (status === "failed") return "destructive"
  return "secondary"
}

function ProbeResult({ probe }: { probe: CustomMetricProbe }) {
  const { t } = useLocale()
  const label = probe.status === "done"
    ? t("custom_metric.probe_status_done")
    : probe.status === "failed" ? t("custom_metric.probe_status_failed") : t("custom_metric.probe_status_pending")
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Badge variant={probeStatusVariant(probe.status)}>{label}</Badge>
        {probe.status === "pending" && <Loader2Icon className="size-3.5 animate-spin text-muted-foreground" />}
      </div>
      {probe.failure && <Alert variant="destructive"><AlertDescription>{probe.failure}</AlertDescription></Alert>}
      {probe.status !== "pending" && (
        <div className="flex flex-col gap-1">
          <Label>{t("custom_metric.probe_output")}</Label>
          <pre className="max-h-48 overflow-auto rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap break-all">
            {probe.output || t("custom_metric.probe_no_output")}
          </pre>
        </div>
      )}
    </div>
  )
}
