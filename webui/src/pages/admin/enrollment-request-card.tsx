import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatDateTime } from "@/lib/intl-format"
import { platformLabel, requestStatusKey } from "@/lib/enrollment-discovery-format"
import { useLocale } from "@/context/locale"
import type { EnrollmentRequest } from "@/types/enrollment"

interface EnrollmentRequestCardProps {
  request: EnrollmentRequest
  busy: boolean
  onApprove?: (request: EnrollmentRequest) => void
  onReject?: (request: EnrollmentRequest) => void
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="truncate" title={value}>{value || "—"}</dd>
    </div>
  )
}

/** 每台机器一张扁平卡片；移动端不嵌套，桌面端按列展开。 */
export function EnrollmentRequestCard({ request, busy, onApprove, onReject }: EnrollmentRequestCardProps) {
  const { locale, t } = useLocale()
  const platform = platformLabel(request.os, request.architecture)
  const decidable = request.status === "pending" || request.status === "approved"
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-3">
        <div className="min-w-0">
          <CardTitle className="truncate" title={request.hostname}>
            {request.hostname || request.node_name || t("discovery.unknown")}
          </CardTitle>
          <p className="mt-1 truncate text-xs text-muted-foreground" title={request.grant_name}>
            {t("discovery.grant")}: {request.grant_name || request.grant_id}
          </p>
        </div>
        <Badge variant={request.status === "claimed" ? "default" : "outline"}>
          {t(requestStatusKey(request.status))}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-3">
        <dl className="grid gap-x-6 gap-y-2 text-xs sm:grid-cols-2 xl:grid-cols-3">
          <Detail label={t("discovery.node_name")} value={request.node_name} />
          <Detail label={t("discovery.source_ip")} value={request.source_ip} />
          <Detail label={t("discovery.platform")} value={platform} />
          <Detail label={t("discovery.agent_version")} value={request.agent_version} />
          <Detail label={t("discovery.advertised_host")} value={request.advertised_host} />
          <Detail label={t("discovery.ssh_port")} value={request.ssh_port > 0 ? String(request.ssh_port) : ""} />
          <Detail label={t("discovery.first_seen")} value={formatDateTime(locale, request.first_request_at)} />
          <Detail label={t("discovery.last_seen")} value={formatDateTime(locale, request.last_request_at)} />
          <Detail label={t("discovery.expires")} value={formatDateTime(locale, request.expires_at)} />
        </dl>
        {request.reason && (
          <p role="status" className="text-xs text-muted-foreground">
            {t("discovery.reason")}: <span className="font-mono">{request.reason}</span>
          </p>
        )}
        {decidable && (
          <div className="flex flex-wrap gap-2">
            {request.status === "pending" && onApprove && (
              <Button size="sm" onClick={() => onApprove(request)} disabled={busy}>{t("discovery.approve")}</Button>
            )}
            {onReject && (
              <Button size="sm" variant="outline" onClick={() => onReject(request)} disabled={busy}>
                {t("discovery.reject")}
              </Button>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
