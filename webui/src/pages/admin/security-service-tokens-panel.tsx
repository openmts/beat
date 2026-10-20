import { useState } from "react"
import { RefreshCwIcon, RotateCwIcon, ShieldOffIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { formatDate, formatDateTime } from "@/lib/intl-format"
import { useApiList } from "@/hooks/use-api-list"
import { useServiceTokenActions } from "@/hooks/use-service-token-actions"
import { getOperationGroups, getOperationNodes } from "@/lib/operations-api"
import { createServiceToken, listServiceTokens, revokeServiceToken, rotateServiceToken } from "@/lib/service-tokens-api"
import { serviceResourceLabel, serviceScopeLabel } from "@/lib/service-token-labels"
import { ServiceTokenActionDialog, ServiceTokenSecretDialog } from "@/pages/admin/service-token-dialogs"
import { ServiceTokenFormDialog } from "@/pages/admin/service-token-form-dialog"
import type { ServiceToken, ServiceTokenRotationInput } from "@/types/service-tokens"

export function SecurityServiceTokensPanel() {
  const { principal } = useAuth()
  if (principal?.user.role !== "owner") return null
  return <ServiceTokensContent />
}

function ServiceTokensContent() {
  const { locale, t } = useLocale()
  const tokens = useApiList(listServiceTokens)
  const nodes = useApiList(getOperationNodes)
  const groups = useApiList(getOperationGroups)
  const flow = useServiceTokenActions()
  const [creating, setCreating] = useState(false)
  const [rotating, setRotating] = useState<ServiceToken | null>(null)
  const [revoking, setRevoking] = useState<ServiceToken | null>(null)
  const refresh = () => void tokens.refresh({ silent: true })
  const submit = (input: ServiceTokenRotationInput, rotate?: ServiceToken) => {
    const title = t(rotate ? "security.tokens.rotate" : "security.tokens.create")
    const execute = rotate ? () => rotateServiceToken(rotate.id, input) : () => createServiceToken(input)
    setCreating(false)
    setRotating(null)
    flow.request({ title, description: t("security.tokens.action_help"), execute: () => execute().then(value => { refresh(); return value }) }, false)
  }
  return <Card>
    <CardHeader className="flex flex-row items-start justify-between gap-3">
      <div className="flex flex-col gap-1">
        <CardTitle>{t("security.tokens.title")}</CardTitle>
        <CardDescription>{t("security.tokens.description")}</CardDescription>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={refresh} disabled={tokens.loading}>
          <RefreshCwIcon data-icon="inline-start" />{t("security.tokens.refresh")}</Button>
        <Button onClick={() => setCreating(true)} disabled={!flow.owner}>{t("security.tokens.create")}</Button>
      </div>
    </CardHeader>
    <CardContent className="flex flex-col gap-4">
      {(tokens.error || flow.error) && <Alert variant="destructive" role="alert"><AlertDescription>{tokens.error ?? t("security.tokens.error")}</AlertDescription></Alert>}
      {tokens.loading && !tokens.data && <div role="status" aria-label={t("security.tokens.loading")} className="space-y-2">
        <Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>}
      {tokens.data?.length === 0 && <p className="text-sm text-muted-foreground">{t("security.tokens.empty")}</p>}
      {tokens.data && tokens.data.length > 0 && <div className="overflow-auto"><Table>
        <TableHeader><TableRow>
          <TableHead>{t("security.tokens.name")}</TableHead><TableHead>{t("security.tokens.status")}</TableHead>
          <TableHead>{t("security.tokens.scopes")}</TableHead><TableHead>{t("security.tokens.resources")}</TableHead>
          <TableHead>{t("security.tokens.expires")}</TableHead><TableHead>{t("security.tokens.last_used")}</TableHead>
          <TableHead>{t("security.tokens.actions")}</TableHead>
        </TableRow></TableHeader>
        <TableBody>{tokens.data.map(token => <TableRow key={token.id}>
          <TableCell><div className="font-medium">{token.name}</div>
            <code className="text-xs text-muted-foreground">{token.prefix}</code></TableCell>
          <TableCell><TokenStatus token={token} /></TableCell>
          <TableCell><div className="flex max-w-56 flex-wrap gap-1">{token.scopes.map(scope =>
            <Badge key={scope} variant="outline">{serviceScopeLabel(scope, t)}</Badge>)}</div></TableCell>
          <TableCell><ResourceSummary token={token} /></TableCell>
          <TableCell className="whitespace-nowrap text-sm">{formatDate(locale, token.expires_at)}</TableCell>
          <TableCell className="whitespace-nowrap text-sm">{token.last_used_at ? formatDateTime(locale, token.last_used_at) : t("security.tokens.never_used")}</TableCell>
          <TableCell><div className="flex gap-1">
            <Button size="sm" variant="outline" aria-label={`${t("security.tokens.rotate")} ${token.name}`}
              disabled={Boolean(token.revoked_at)} onClick={() => setRotating(token)}>
              <RotateCwIcon data-icon="inline-start" />{t("security.tokens.rotate")}</Button>
            <Button size="sm" variant="outline" aria-label={`${t("security.tokens.revoke")} ${token.name}`}
              disabled={Boolean(token.revoked_at)} onClick={() => setRevoking(token)}>
              <ShieldOffIcon data-icon="inline-start" />{t("security.tokens.revoke")}</Button>
          </div></TableCell>
        </TableRow>)}</TableBody>
      </Table></div>}
    </CardContent>
    <ServiceTokenFormDialog key={creating ? "create-open" : "create-idle"} mode="create" open={creating}
      nodes={nodes.data ?? []} groups={groups.data ?? []} onCancel={() => setCreating(false)}
      onSubmit={input => submit(input)} />
    <ServiceTokenFormDialog key={rotating ? `rotate-${rotating.id}` : "rotate-idle"} mode="rotate"
      token={rotating ?? undefined} open={rotating !== null} nodes={nodes.data ?? []} groups={groups.data ?? []}
      onCancel={() => setRotating(null)} onSubmit={input => { if (rotating) submit(input, rotating) }} />
    <RevokeDialog token={revoking} onCancel={() => setRevoking(null)} onConfirm={reason => {
      const token = revoking
      setRevoking(null)
      if (token) flow.request({ title: t("security.tokens.revoke"), description: t("security.tokens.revoke_help"),
        execute: () => revokeServiceToken(token.id, reason).then(() => { refresh() }) }, true)
    }} />
    <ServiceTokenActionDialog flow={flow} />
    <ServiceTokenSecretDialog credential={flow.result} dismiss={flow.dismissResult} />
  </Card>
}

function TokenStatus({ token }: { token: ServiceToken }) {
  const { t } = useLocale()
  const status = token.revoked_at ? "status_revoked" : Date.parse(token.expires_at) <= Date.now() ? "status_expired"
    : token.superseded_at ? "status_rotating" : "status_active"
  const variant = status === "status_active" ? "default" : status === "status_rotating" ? "secondary" : "destructive"
  return <Badge variant={variant}>{t(`security.tokens.${status}`)}</Badge>
}

function ResourceSummary({ token }: { token: ServiceToken }) {
  const { t } = useLocale()
  const resources = token.resources ?? []
  if (resources.length === 0) return <span className="text-sm text-muted-foreground">{t("security.tokens.no_resources")}</span>
  return <div className="flex flex-col gap-1">
    {resources.slice(0, 4).map(resource => <span key={`${resource.kind}:${resource.id}`} className="text-xs">
      {serviceResourceLabel(resource.kind, t)} <code>{resource.id}</code></span>)}
    {resources.length > 4 && <span className="text-xs text-muted-foreground">+{resources.length - 4}</span>}
    {token.source_cidrs.length > 0 && <span className="text-xs text-muted-foreground">{token.source_cidrs.join(", ")}</span>}
  </div>
}

function RevokeDialog({ token, onCancel, onConfirm }: { token: ServiceToken | null; onCancel: () => void; onConfirm: (reason: string) => void }) {
  const { t } = useLocale()
  const [reason, setReason] = useState("")
  return <Dialog open={token !== null} onOpenChange={open => { if (!open) { setReason(""); onCancel() } }}>
    <DialogContent><DialogHeader><DialogTitle>{t("security.tokens.revoke_confirm")}</DialogTitle>
      <DialogDescription>{t("security.tokens.revoke_help")}</DialogDescription></DialogHeader>
      {token && <FieldGroup>
        <Field><FieldLabel htmlFor="service-token-revoke-reason">{t("security.tokens.revoke_reason")}</FieldLabel>
          <Input id="service-token-revoke-reason" maxLength={512} value={reason} onChange={event => setReason(event.target.value)} /></Field>
        <div className="flex flex-wrap gap-2">
          <Button variant="destructive" disabled={!reason.trim()} onClick={() => { onConfirm(reason.trim()); setReason("") }}>
            {t("security.tokens.revoke")}</Button>
          <Button variant="outline" onClick={() => { setReason(""); onCancel() }}>{t("app.cancel")}</Button>
        </div>
      </FieldGroup>}</DialogContent>
  </Dialog>
}
