import { useEffect, useState } from "react"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from "@/components/ui/dialog"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { PlusIcon, Trash2Icon, PencilIcon, RefreshCwIcon, Link2Icon, ChartLineIcon } from "lucide-react"
import { PageHeader } from "@/components/page-header"
import { useNodes } from "@/hooks/use-api"
import { getNodeMetrics } from "@/lib/api"
import { MetricsChart } from "@/components/metrics-chart"
import type { CustomMetric, CustomMetricPayload, MetricDataPoint } from "@/types"

const intervalOptions = [
  { label: "5s", value: 5 },
  { label: "10s", value: 10 },
  { label: "15s", value: 15 },
  { label: "30s", value: 30 },
  { label: "60s", value: 60 },
  { label: "5m", value: 300 },
  { label: "15m", value: 900 },
  { label: "1h", value: 3600 },
]

function CustomMetrics() {
  const { t } = useLocale()
  const [metrics, setMetrics] = useState<CustomMetric[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<CustomMetric | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [bindMetric, setBindMetric] = useState<CustomMetric | null>(null)
  const [bindNodes, setBindNodes] = useState<string[]>([])
  const [bindLoading, setBindLoading] = useState(false)
  const [viewMetric, setViewMetric] = useState<CustomMetric | null>(null)
  const [viewData, setViewData] = useState<Record<string, MetricDataPoint[]>>({})
  const [viewLoading, setViewLoading] = useState(false)

  const { data: nodes, loading: nodesLoading } = useNodes()
  const onlineNodes = nodes?.filter((n) => n.status === "online") ?? []

  const [form, setForm] = useState<CustomMetricPayload>({
    name: "",
    shell_command: "",
    interval_seconds: 30,
    description: "",
    enabled: true,
  })

  const refresh = async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.listCustomMetrics()
      setMetrics(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void refresh() }, [])

  const openCreate = () => {
    setEditing(null)
    setForm({ name: "", shell_command: "", interval_seconds: 30, description: "", enabled: true })
    setActionError(null)
    setDialogOpen(true)
  }

  const openEdit = (metric: CustomMetric) => {
    setEditing(metric)
    setForm({
      name: metric.name,
      shell_command: metric.shell_command,
      interval_seconds: metric.interval_seconds,
      description: metric.description,
      enabled: metric.enabled,
    })
    setActionError(null)
    setDialogOpen(true)
  }

  const handleSave = async () => {
    if (!form.name.trim() || !form.shell_command.trim()) return
    setActionLoading(true)
    setActionError(null)
    try {
      if (editing) {
        await api.updateCustomMetric(editing.id, form)
      } else {
        await api.createCustomMetric(form)
      }
      setDialogOpen(false)
      await refresh()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setActionLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteId) return
    setActionLoading(true)
    setActionError(null)
    try {
      await api.deleteCustomMetric(deleteId)
      setDeleteId(null)
      await refresh()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setActionLoading(false)
    }
  }

  const openBind = async (metric: CustomMetric) => {
    setBindMetric(metric)
    setBindLoading(true)
    setActionError(null)
    try {
      const ids = await api.listCustomMetricNodes(metric.id)
      setBindNodes(ids)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setBindLoading(false)
    }
  }

  const handleBindToggle = (nodeId: string) => {
    setBindNodes((prev) => prev.includes(nodeId) ? prev.filter((id) => id !== nodeId) : [...prev, nodeId])
  }

  const handleSaveBindings = async () => {
    if (!bindMetric) return
    setBindLoading(true)
    setActionError(null)
    try {
      await api.setCustomMetricNodes(bindMetric.id, bindNodes)
      setBindMetric(null)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setBindLoading(false)
    }
  }

  const openView = async (metric: CustomMetric) => {
    setViewMetric(metric)
    setViewLoading(true)
    setViewData({})
    try {
      const bound = await api.listCustomMetricNodes(metric.id)
      const to = new Date()
      const from = new Date(to.getTime() - 6 * 3600 * 1000)
      const result: Record<string, MetricDataPoint[]> = {}
      for (const nodeId of bound) {
        const data = await getNodeMetrics(nodeId, [`custom_${metric.name}`], from.toISOString(), to.toISOString())
        const points = data[`custom_${metric.name}`] ?? []
        if (points.length > 0) result[nodeId] = points
      }
      setViewData(result)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setViewLoading(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("custom_metric.title")} description={t("custom_metric.description")}>
        <Button onClick={openCreate}>
          <PlusIcon data-icon="inline-start" />
          {t("custom_metric.create")}
        </Button>
        <Button variant="outline" onClick={() => void refresh()}>
          <RefreshCwIcon data-icon="inline-start" />
          {t("app.refresh")}
        </Button>
      </PageHeader>

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardContent>
          {loading ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          ) : metrics.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("custom_metric.empty")}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("custom_metric.name")}</TableHead>
                  <TableHead>{t("custom_metric.shell_command")}</TableHead>
                  <TableHead>{t("custom_metric.interval")}</TableHead>
                  <TableHead>{t("custom_metric.status")}</TableHead>
                  <TableHead className="text-right">{t("app.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {metrics.map((metric) => (
                  <TableRow key={metric.id}>
                    <TableCell className="font-medium">{metric.name}</TableCell>
                    <TableCell className="max-w-[280px] truncate font-mono text-xs text-muted-foreground">
                      {metric.shell_command}
                    </TableCell>
                    <TableCell className="tabular-nums">{metric.interval_seconds}s</TableCell>
                    <TableCell>
                      <Badge variant={metric.enabled ? "default" : "secondary"}>
                        {metric.enabled ? t("custom_metric.enabled") : t("custom_metric.disabled")}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="icon" aria-label={t("custom_metric.view_data")}
                          onClick={() => void openView(metric)}>
                          <ChartLineIcon data-icon />
                        </Button>
                        <Button variant="ghost" size="icon" aria-label={t("custom_metric.bind_nodes")}
                          onClick={() => void openBind(metric)}>
                          <Link2Icon data-icon />
                        </Button>
                        <Button variant="ghost" size="icon" aria-label={t("app.edit")}
                          onClick={() => openEdit(metric)}>
                          <PencilIcon data-icon />
                        </Button>
                        <Button variant="ghost" size="icon" aria-label={t("app.delete")}
                          onClick={() => setDeleteId(metric.id)}>
                          <Trash2Icon data-icon />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? t("custom_metric.edit") : t("custom_metric.create")}</DialogTitle>
            <DialogDescription>{t("custom_metric.form_description")}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-4">
            {actionError && <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>}
            <div className="flex flex-col gap-2">
              <Label htmlFor="cm-name">{t("custom_metric.name")}</Label>
              <Input id="cm-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="cm-command">{t("custom_metric.shell_command")}</Label>
              <Textarea id="cm-command" value={form.shell_command}
                onChange={(e) => setForm({ ...form, shell_command: e.target.value })}
                placeholder="e.g. curl -so /dev/null -w '%{time_total}' https://example.com" />
            </div>
            <div className="flex flex-col gap-2">
              <Label>{t("custom_metric.interval")}</Label>
              <Select
                items={intervalOptions.map((o) => ({ label: o.label, value: String(o.value) }))}
                value={String(form.interval_seconds)}
                onValueChange={(v) => { if (v) setForm({ ...form, interval_seconds: Number(v) }) }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {intervalOptions.map((option) => (
                      <SelectItem key={option.value} value={String(option.value)}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="cm-desc">{t("custom_metric.description_label")}</Label>
              <Input id="cm-desc" value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>{t("app.cancel")}</Button>
            <Button onClick={handleSave} disabled={actionLoading || !form.name.trim() || !form.shell_command.trim()}>
              {t("app.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteId} onOpenChange={(o) => { if (!o) setDeleteId(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("custom_metric.delete_title")}</DialogTitle>
            <DialogDescription>{t("custom_metric.delete_description")}</DialogDescription>
          </DialogHeader>
          {actionError && <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteId(null)}>{t("app.cancel")}</Button>
            <Button variant="destructive" onClick={() => void handleDelete()} disabled={actionLoading}>
              {t("app.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!bindMetric} onOpenChange={(o) => { if (!o) setBindMetric(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("custom_metric.bind_nodes")}</DialogTitle>
            <DialogDescription>{t("custom_metric.bind_description")}</DialogDescription>
          </DialogHeader>
          {actionError && <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>}
          {bindLoading && !nodesLoading ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : (
            <div className="flex max-h-72 flex-col gap-1 overflow-auto">
              {onlineNodes.length === 0 ? (
                <p className="py-4 text-center text-sm text-muted-foreground">{t("terminal.no_online_nodes")}</p>
              ) : (
                onlineNodes.map((node) => {
                  const checked = bindNodes.includes(node.id)
                  return (
                    <button
                      key={node.id}
                      type="button"
                      onClick={() => handleBindToggle(node.id)}
                      className="flex items-center gap-3 rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
                    >
                      <span className={`flex size-4 shrink-0 items-center justify-center rounded border ${checked ? "border-primary bg-primary text-primary-foreground" : "border-input"}`}>
                        {checked && <span className="text-[10px] font-bold">✓</span>}
                      </span>
                      <span className="truncate">{node.alias || node.name}</span>
                      <span className="ml-auto truncate text-xs text-muted-foreground">{node.host}</span>
                    </button>
                  )
                })
              )}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setBindMetric(null)}>{t("app.cancel")}</Button>
            <Button onClick={() => void handleSaveBindings()} disabled={bindLoading}>
              {t("app.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!viewMetric} onOpenChange={(o) => { if (!o) setViewMetric(null) }}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{viewMetric ? `${viewMetric.name} ${t("custom_metric.view_data")}` : ""}</DialogTitle>
            <DialogDescription>{t("custom_metric.view_description")}</DialogDescription>
          </DialogHeader>
          {actionError && <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>}
          {viewLoading ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-48 w-full" />
              <Skeleton className="h-48 w-full" />
            </div>
          ) : Object.keys(viewData).length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("custom_metric.no_data")}</p>
          ) : (
            <div className="flex max-h-[60vh] flex-col gap-4 overflow-auto">
              {Object.entries(viewData).map(([nodeId, points]) => {
                const node = nodes?.find((n) => n.id === nodeId)
                return (
                  <div key={nodeId}>
                    <h3 className="mb-2 text-sm font-medium">{node?.alias || node?.name || nodeId}</h3>
                    <MetricsChart
                      data={points}
                      title=""
                      color="#2563eb"
                      valueFormat="number"
                      from={points[0]?.timestamp ? Number(points[0].timestamp) : 0}
                      to={points[points.length - 1]?.timestamp ? Number(points[points.length - 1].timestamp) : 0}
                      rangeHours={6}
                    />
                  </div>
                )
              })}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setViewMetric(null)}>{t("app.close")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export default CustomMetrics
