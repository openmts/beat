import { useState, type FormEvent } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import { defaultGrantExpiry, localInstantInput, parseLocalInstant } from "@/lib/enrollment-discovery-format"
import { EnrollmentGrantFields } from "./enrollment-grant-fields"
import type { EnrollmentGrant, EnrollmentGrantWrite } from "@/types/enrollment"
import type { Group } from "@/types"

const minimumEnrollments = 1
const maximumEnrollments = 1000
const maximumSSHPort = 65535

export function grantDraft(grant: EnrollmentGrant | undefined, groups: Group[]): EnrollmentGrantWrite {
  if (!grant) {
    return { name: "", mode: "manual", enabled: true, expires_at: defaultGrantExpiry(),
      default_group_id: groups.find((group) => group.is_default)?.id ?? "", node_name_prefix: "", tags: [],
      remarks: "", public: false, ssh_port: 22, source_ip_as_host: false, source_cidrs: [],
      hostname_pattern: "", claim_recovery: true, maximum_enrollments: 10 }
  }
  return { name: grant.name, mode: grant.mode, enabled: grant.enabled, expires_at: localInstantInput(grant.expires_at),
    default_group_id: grant.default_group_id, node_name_prefix: grant.node_name_prefix,
    tags: [...grant.tags], remarks: grant.remarks, public: grant.public, ssh_port: grant.ssh_port,
    source_ip_as_host: grant.source_ip_as_host, source_cidrs: [...grant.source_cidrs],
    hostname_pattern: grant.hostname_pattern, claim_recovery: grant.claim_recovery,
    maximum_enrollments: grant.maximum_enrollments }
}

function validate(draft: EnrollmentGrantWrite, t: (key: string) => string): string {
  if (!draft.name.trim()) return t("discovery.grant_name_required")
  if (!draft.default_group_id) return t("discovery.group_required")
  if (!Number.isFinite(Date.parse(draft.expires_at))) return t("discovery.expiry_required")
  if (draft.maximum_enrollments < minimumEnrollments || draft.maximum_enrollments > maximumEnrollments) {
    return t("discovery.capacity_invalid")
  }
  if (draft.ssh_port < 1 || draft.ssh_port > maximumSSHPort) return t("discovery.port_invalid")
  return ""
}

interface EnrollmentGrantDialogProps {
  grant?: EnrollmentGrant
  groups: Group[]
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (payload: EnrollmentGrantWrite) => void
}

/** 创建与覆盖共用同一份策略表单；提交本身由所有者的最近重新认证流程执行。 */
export function EnrollmentGrantDialog({ grant, groups, busy, error, onClose, onSubmit }: EnrollmentGrantDialogProps) {
  const { t } = useLocale()
  const [draft, setDraft] = useState(() => grantDraft(grant, groups))
  const [invalid, setInvalid] = useState("")
  const change = <K extends keyof EnrollmentGrantWrite>(key: K, value: EnrollmentGrantWrite[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (busy) return
    const failure = validate(draft, t)
    if (failure) {
      setInvalid(failure)
      return
    }
    setInvalid("")
    onSubmit({ ...draft, name: draft.name.trim(), expires_at: parseLocalInstant(draft.expires_at) })
  }
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-h-[85vh] overflow-auto sm:max-w-2xl">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{t(grant ? "discovery.edit_grant" : "discovery.create_grant")}</DialogTitle>
            <DialogDescription>{t("discovery.grants_hint")}</DialogDescription>
          </DialogHeader>
          <EnrollmentGrantFields draft={draft} groups={groups} disabled={busy} change={change} />
          {(invalid || error) && <Alert variant="destructive"><AlertDescription>{invalid || error}</AlertDescription></Alert>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>{t("app.cancel")}</Button>
            <Button type="submit" disabled={busy}>{t("app.save")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
