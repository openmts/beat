import { useState, type FormEvent } from "react"
import { AdminSelectField } from "@/components/admin-select-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { approveEnrollmentRequest } from "@/lib/enrollment-discovery-api"
import { joinEntries, splitEntries } from "@/lib/enrollment-discovery-format"
import type { EnrollmentGrant, EnrollmentRequest } from "@/types/enrollment"
import type { Group } from "@/types"

interface ApprovalDraft {
  nodeName: string
  groupID: string
  labels: string
  remarks: string
  isPublic: boolean
  host: string
  sshPort: string
}

const maximumSSHPort = 65535

function initialDraft(request: EnrollmentRequest, grant: EnrollmentGrant | undefined, groups: Group[]): ApprovalDraft {
  const defaultGroup = grant?.default_group_id || groups.find((group) => group.is_default)?.id || ""
  return {
    nodeName: request.node_name || request.hostname,
    groupID: defaultGroup,
    labels: joinEntries(grant?.tags),
    remarks: grant?.remarks ?? "",
    isPublic: grant?.public ?? false,
    host: request.advertised_host || request.source_ip,
    sshPort: String(request.ssh_port > 0 ? request.ssh_port : grant?.ssh_port ?? 22),
  }
}

function validate(draft: ApprovalDraft, t: (key: string) => string): string {
  if (!draft.nodeName.trim()) return t("discovery.name_required")
  if (!draft.groupID) return t("discovery.group_required")
  const port = Number(draft.sshPort)
  if (!Number.isInteger(port) || port < 1 || port > maximumSSHPort) return t("discovery.port_invalid")
  return ""
}

interface EnrollmentApprovalDialogProps {
  request: EnrollmentRequest
  grant: EnrollmentGrant | undefined
  groups: Group[]
  onClose: () => void
  onApproved: (request: EnrollmentRequest) => void
}

/** 审批弹窗只提交最终节点字段；预留节点里的凭据仍由机器认领时生成。 */
export function EnrollmentApprovalDialog({ request, grant, groups, onClose, onApproved }: EnrollmentApprovalDialogProps) {
  const { t } = useLocale()
  const [draft, setDraft] = useState(() => initialDraft(request, grant, groups))
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const change = <K extends keyof ApprovalDraft>(key: K, value: ApprovalDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (busy) return
    const invalid = validate(draft, t)
    if (invalid) {
      setError(invalid)
      return
    }
    setError("")
    setBusy(true)
    try {
      onApproved(await approveEnrollmentRequest(request.id, {
        node_name: draft.nodeName.trim(), group_id: draft.groupID, tags: splitEntries(draft.labels),
        remarks: draft.remarks, public: draft.isPublic, host: draft.host.trim(), ssh_port: Number(draft.sshPort),
      }))
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("discovery.grant_error"))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-h-[85vh] overflow-auto sm:max-w-xl">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{t("discovery.approve_title")}</DialogTitle>
            <DialogDescription>{request.hostname || request.source_ip}</DialogDescription>
          </DialogHeader>
          <fieldset disabled={busy} className="py-4">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="enrollment-node-name">{t("discovery.node_name")}</FieldLabel>
                <Input id="enrollment-node-name" value={draft.nodeName} maxLength={100}
                  onChange={(event) => change("nodeName", event.target.value)} />
              </Field>
              <AdminSelectField id="enrollment-group" label={t("discovery.default_group")} value={draft.groupID}
                options={[{ value: "", label: t("discovery.group_required") },
                  ...groups.map((group) => ({ value: group.id, label: group.name }))]}
                onValueChange={(value) => change("groupID", value)} />
              <Field>
                <FieldLabel htmlFor="enrollment-labels">{t("discovery.labels")}</FieldLabel>
                <Input id="enrollment-labels" value={draft.labels}
                  onChange={(event) => change("labels", event.target.value)} />
                <p className="text-xs text-muted-foreground">{t("discovery.labels_hint")}</p>
              </Field>
              <Field>
                <FieldLabel htmlFor="enrollment-host">{t("discovery.advertised_host")}</FieldLabel>
                <Input id="enrollment-host" value={draft.host} maxLength={253}
                  onChange={(event) => change("host", event.target.value)} />
              </Field>
              <Field>
                <FieldLabel htmlFor="enrollment-ssh-port">{t("discovery.ssh_port")}</FieldLabel>
                <Input id="enrollment-ssh-port" inputMode="numeric" value={draft.sshPort}
                  onChange={(event) => change("sshPort", event.target.value)} />
              </Field>
              <Field>
                <FieldLabel htmlFor="enrollment-remarks">{t("discovery.remarks")}</FieldLabel>
                <Textarea id="enrollment-remarks" value={draft.remarks} maxLength={2000}
                  onChange={(event) => change("remarks", event.target.value)} />
              </Field>
              <Label className="flex items-start gap-3 rounded-lg border p-3 leading-relaxed">
                <Checkbox checked={draft.isPublic} className="mt-0.5"
                  onCheckedChange={(checked) => change("isPublic", checked === true)} />
                <span>{t("discovery.public")}</span>
              </Label>
            </FieldGroup>
          </fieldset>
          {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>{t("app.cancel")}</Button>
            <Button type="submit" disabled={busy}>{t("discovery.approve")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
