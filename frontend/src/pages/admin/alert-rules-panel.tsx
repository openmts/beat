import { useState } from "react"
import { useAlertRules } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"
import type { AlertRule } from "@/types"
import { AlertRuleDialog, type AlertRuleForm } from "@/pages/admin/alert-rule-dialog"
import {
  buildAlertMetricOptions,
  formatAlertCondition,
  messageFromError,
  severityColors,
} from "@/pages/admin/alert-utils"

function AlertRulesPanel() {
  const { data: rules, loading, error, refresh } = useAlertRules()
  const { t } = useLocale()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [deleteId, setDeleteId] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [form, setForm] = useState<AlertRuleForm>({
    name: "",
    description: "",
    metric: "cpu",
    operator: "gt",
    threshold: 80,
    duration: 300,
    severity: "warning",
  })
  const metricOptions = buildAlertMetricOptions(t)
  const metricLabels = new Map(metricOptions.map((option) => [option.value, option.label]))

  const resetForm = () => setForm({
    name: "", description: "", metric: "cpu", operator: "gt",
    threshold: 80, duration: 300, severity: "warning",
  })

  const handleSave = async () => {
    if (!form.name.trim()) return
    setActionError(null)
    setActionLoading(true)
    try {
      if (editing) {
        await api.updateAlertRule(editing.id, { ...form, enabled: editing.enabled })
      } else {
        await api.createAlertRule({ ...form, enabled: true })
      }
      await refresh()
      setDialogOpen(false)
      setEditing(null)
      resetForm()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  const handleToggle = async (rule: AlertRule) => {
    setActionError(null)
    try {
      await api.updateAlertRule(rule.id, { ...rule, enabled: !rule.enabled })
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    }
  }

  const handleDelete = async () => {
    if (!deleteId) return
    setActionError(null)
    setActionLoading(true)
    try {
      await api.deleteAlertRule(deleteId)
      await refresh()
      setDeleteId(null)
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setActionLoading(false)
    }
  }

  if (error) {
    return <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Button
          onClick={() => {
            resetForm()
            setEditing(null)
            setDialogOpen(true)
          }}
        >
          <PlusIcon data-icon="inline-start" />
          {t("app.create")}
        </Button>
      </div>
      {actionError && (
        <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>
      )}

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("node.name")}</TableHead>
                <TableHead>{t("alert.metric")}</TableHead>
                <TableHead>{t("alert.condition")}</TableHead>
                <TableHead>{t("alert.severity")}</TableHead>
                <TableHead>{t("alert.enabled")}</TableHead>
                <TableHead className="w-32">{t("app.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-10" /></TableCell>
                    <TableCell><Skeleton className="h-8 w-24" /></TableCell>
                  </TableRow>
                ))
              ) : rules && rules.length > 0 ? (
                rules.map((rule) => (
                  <TableRow key={rule.id}>
                    <TableCell className="font-medium">{rule.name}</TableCell>
                    <TableCell>{metricLabels.get(rule.metric) ?? rule.metric}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {formatAlertCondition(rule, t)}
                    </TableCell>
                    <TableCell>
                      <Badge variant={severityColors[rule.severity] ?? "default"}>
                        {rule.severity}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={rule.enabled}
                        onCheckedChange={() => handleToggle(rule)}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => {
                            setForm({
                              name: rule.name,
                              description: rule.description,
                              metric: rule.metric,
                              operator: rule.operator,
                              threshold: rule.threshold,
                              duration: rule.duration,
                              severity: rule.severity,
                            })
                            setEditing(rule)
                            setDialogOpen(true)
                          }}
                        >
                          <PencilIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setDeleteId(rule.id)}
                        >
                          <Trash2Icon className="text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground">
                    {t("app.no_data")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <AlertRuleDialog
        open={dialogOpen}
        editing={editing !== null}
        form={form}
        loading={actionLoading}
        onOpenChange={setDialogOpen}
        onChange={setForm}
        onSave={handleSave}
      />

      <Dialog open={!!deleteId} onOpenChange={(v) => { if (!v) setDeleteId(null) }}>
        <DialogContent>
          <DialogHeader><DialogTitle>{t("confirm.delete")}</DialogTitle></DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteId(null)}>{t("app.cancel")}</Button>
            <Button variant="destructive" onClick={handleDelete} disabled={actionLoading}>
              {t("app.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}


export default AlertRulesPanel
