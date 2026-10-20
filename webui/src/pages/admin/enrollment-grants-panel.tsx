import { useState, type ReactNode } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useGroups } from "@/hooks/use-api"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { formatDateTime } from "@/lib/intl-format"
import { grantState } from "@/lib/enrollment-discovery-format"
import { createEnrollmentGrant, revokeEnrollmentGrant, rotateEnrollmentGrantSecret, updateEnrollmentGrant } from "@/lib/enrollment-discovery-api"
import { EnrollmentGrantDialog } from "./enrollment-grant-dialog"
import { OperationConfirm } from "./operation-confirm"
import { EnrollmentProofDialog } from "./enrollment-proof-dialog"
import { EnrollmentSecretDialog } from "./enrollment-secret-dialog"
import { useEnrollmentGrants } from "./enrollment-data"
import type { EnrollmentGrant, EnrollmentGrantSecret, EnrollmentGrantWrite } from "@/types/enrollment"

interface SecretView {
  value: EnrollmentGrantSecret
  created: boolean
}

const stateKeys: Record<string, string> = {
  active: "discovery.active", inactive: "discovery.inactive",
  expired: "discovery.status_expired", revoked: "discovery.revoked",
}

function GrantRow({ grant, children }: { grant: EnrollmentGrant; children: ReactNode }) {
  const { locale, t } = useLocale()
  const state = grantState(grant, Date.now())
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-3">
        <div className="min-w-0">
          <CardTitle className="truncate" title={grant.name}>{grant.name}</CardTitle>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(grant.mode === "automatic" ? "discovery.mode_automatic" : "discovery.mode_manual")} ·{" "}
            {grant.default_group_name || grant.default_group_id} ·{" "}
            {t("discovery.slots", { used: grant.consumed_enrollments, max: grant.maximum_enrollments })}
          </p>
        </div>
        <Badge variant={state === "active" ? "default" : "outline"}>{t(stateKeys[state] ?? "discovery.inactive")}</Badge>
      </CardHeader>
      <CardContent className="space-y-3">
        <dl className="grid gap-x-6 gap-y-2 text-xs sm:grid-cols-2 xl:grid-cols-3">
          <div><dt className="text-muted-foreground">{t("discovery.node_name_prefix")}</dt><dd className="truncate">{grant.node_name_prefix || "—"}</dd></div>
          <div><dt className="text-muted-foreground">{t("discovery.expiry")}</dt><dd>{formatDateTime(locale, grant.expires_at)}</dd></div>
          <div><dt className="text-muted-foreground">{t("discovery.source_scope")}</dt><dd className="truncate" title={grant.source_cidrs.join(", ")}>
            {grant.source_cidrs.length > 0 ? grant.source_cidrs.join(", ") : t("app.all")}
            {grant.hostname_pattern ? ` · ${grant.hostname_pattern}` : ""}
          </dd></div>
          <div><dt className="text-muted-foreground">{t("discovery.claim_recovery")}</dt><dd>{grant.claim_recovery ? t("alert.enabled") : t("alert.disabled")}</dd></div>
          <div><dt className="text-muted-foreground">{t("discovery.grant")}</dt><dd className="font-mono">{grant.secret_prefix}</dd></div>
        </dl>
        <div className="flex flex-wrap gap-2">{children}</div>
      </CardContent>
    </Card>
  )
}

/** 授权面板：创建、覆盖、轮换秘密与撤销都要所有者身份与最近重新认证。 */
export function EnrollmentGrantsPanel() {
  const { t } = useLocale()
  const { principal } = useAuth()
  const groups = useGroups()
  const grants = useEnrollmentGrants(true)
  const flow = useConfirmedOperation()
  const [editing, setEditing] = useState<EnrollmentGrant | null | undefined>(undefined)
  const [secret, setSecret] = useState<SecretView | null>(null)
  const [notice, setNotice] = useState("")
  // 授权读写都需要近期重认证；428 时改为就地确认身份，而不是留一个永远失败的重试按钮。
  const [proof, setProof] = useState(false)
  const needsProof = grants.status === 428
  const refresh = grants.refresh

  // 表单先关闭再确认：确认弹窗不能嵌在表单弹窗里（嵌套模态会互相 aria-hidden）。
  const save = (payload: EnrollmentGrantWrite) => {
    const creating = editing === null
    const target = editing ?? undefined
    setEditing(undefined)
    flow.request({
      title: t(creating ? "discovery.create_grant" : "discovery.edit_grant"),
      description: t("discovery.grants_hint"),
      execute: async () => {
        const created = creating ? await createEnrollmentGrant(payload) : null
        if (!creating && target) await updateEnrollmentGrant(target.id, payload)
        setSecret(created ? { value: created, created: true } : null)
        setNotice(t(creating ? "discovery.grant_created" : "discovery.grant_updated"))
        await refresh({ silent: true })
      },
    })
  }
  const rotate = (grant: EnrollmentGrant) => flow.request({
    title: t("discovery.rotate_title"), description: t("discovery.rotate_warning"),
    execute: async () => {
      const rotated = await rotateEnrollmentGrantSecret(grant.id)
      await refresh({ silent: true })
      setSecret({ value: rotated, created: false })
      setNotice(t("discovery.grant_updated"))
    },
  })
  const revoke = (grant: EnrollmentGrant) => flow.request({
    title: t("discovery.revoke_title"), description: t("discovery.revoke_warning"),
    execute: async () => {
      await revokeEnrollmentGrant(grant.id)
      await refresh({ silent: true })
      setNotice(t("discovery.grant_revoked"))
    },
  })
  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">{t("discovery.grants_hint")}</p>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => void refresh({ silent: true })}>{t("app.refresh")}</Button>
          <Button size="sm" onClick={() => setEditing(null)}>{t("discovery.create_grant")}</Button>
        </div>
      </div>
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {needsProof && <Alert><AlertDescription className="flex flex-wrap items-center gap-2">
        {t("discovery.proof_required")}
        <Button size="sm" onClick={() => setProof(true)}>{t("discovery.proof_action")}</Button>
      </AlertDescription></Alert>}
      {grants.error && !needsProof && <Alert variant="destructive"><AlertDescription>
        {grants.error}
        <Button variant="outline" size="sm" className="ml-3" onClick={() => void refresh()}>{t("app.retry")}</Button>
      </AlertDescription></Alert>}
      {grants.loading && <Skeleton className="h-40" />}
      {grants.data?.length === 0 && <p className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
        {t("discovery.empty_grants")}
      </p>}
      {grants.data?.map((grant) => (
        <GrantRow key={grant.id} grant={grant}>
          <Button size="sm" variant="outline" onClick={() => setEditing(grant)}>{t("discovery.edit_grant")}</Button>
          <Button size="sm" variant="outline" onClick={() => rotate(grant)}>{t("discovery.rotate")}</Button>
          <Button size="sm" variant="outline" onClick={() => revoke(grant)}>{t("discovery.revoke")}</Button>
        </GrantRow>
      ))}
      {editing !== undefined && <EnrollmentGrantDialog grant={editing ?? undefined} groups={groups.data ?? []}
        busy={flow.busy} error={flow.error} onClose={() => setEditing(undefined)} onSubmit={save} />}
      {secret && <EnrollmentSecretDialog value={secret.value} created={secret.created} onClose={() => setSecret(null)} />}
      {proof && <EnrollmentProofDialog codeRequired={Boolean(principal?.user.totp_enabled_at)}
        onClose={() => setProof(false)} onConfirmed={() => void refresh()} />}
      <OperationConfirm flow={flow} />
    </section>
  )
}
