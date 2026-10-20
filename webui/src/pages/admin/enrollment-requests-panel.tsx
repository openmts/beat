import { useCallback, useEffect, useState } from "react"
import { AdminSelectField } from "@/components/admin-select-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useApiPage } from "@/hooks/use-api-page"
import { useGroups } from "@/hooks/use-api"
import { listEnrollmentRequests } from "@/lib/enrollment-discovery-api"
import { requestStatusKey } from "@/lib/enrollment-discovery-format"
import { EnrollmentApprovalDialog } from "./enrollment-approval-dialog"
import { EnrollmentRejectDialog } from "./enrollment-reject-dialog"
import { EnrollmentRequestCard } from "./enrollment-request-card"
import { useEnrollmentGrants } from "./enrollment-data"
import type { EnrollmentRequest, EnrollmentRequestStatus } from "@/types/enrollment"

const pendingPollMillis = 15_000
const pageSize = 25

const recentStatuses: EnrollmentRequestStatus[] = ["pending", "approved", "rejected", "expired", "claimed"]

interface EnrollmentRequestsPanelProps {
  mode: "pending" | "recent"
}

export function EnrollmentRequestsPanel({ mode }: EnrollmentRequestsPanelProps) {
  const { t } = useLocale()
  const { principal } = useAuth()
  const [status, setStatus] = useState<EnrollmentRequestStatus | "">("")
  const fetcher = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const query = { status: mode === "pending" ? ("pending" as const) : status, cursor, limit: pageSize }
    const result = await listEnrollmentRequests(query, signal)
    return { items: result.requests, next_cursor: result.next_cursor }
  }, [mode, status])
  const page = useApiPage<EnrollmentRequest>(fetcher)
  const groups = useGroups()
  const grants = useEnrollmentGrants(principal?.user.role === "owner")
  const [approving, setApproving] = useState<EnrollmentRequest | null>(null)
  const [rejecting, setRejecting] = useState<EnrollmentRequest | null>(null)
  const [notice, setNotice] = useState("")
  const refresh = page.refresh

  useEffect(() => {
    if (mode !== "pending") return
    const timer = window.setInterval(() => { void refresh({ silent: true }) }, pendingPollMillis)
    return () => window.clearInterval(timer)
  }, [mode, refresh])

  const grantFor = (request: EnrollmentRequest) => grants.data?.find((grant) => grant.id === request.grant_id)
  const decided = (message: string) => {
    setApproving(null)
    setRejecting(null)
    setNotice(message)
    void refresh({ silent: true })
  }
  const empty = page.data?.length === 0 && !page.loading

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">
          {t(mode === "pending" ? "discovery.pending_hint" : "discovery.recent_hint")}
        </p>
        <Button variant="outline" size="sm" onClick={() => void refresh({ silent: true })}>{t("app.refresh")}</Button>
      </div>
      {mode === "recent" && (
        <div className="max-w-xs">
          <AdminSelectField id="enrollment-status" label={t("discovery.status_filter")} value={status || "all"}
            options={[{ value: "all", label: t("discovery.all_statuses") },
              ...recentStatuses.map((value) => ({ value, label: t(requestStatusKey(value)) }))]}
            onValueChange={(value) => setStatus(value === "all" ? "" : value as EnrollmentRequestStatus)} />
        </div>
      )}
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {page.error && <Alert variant="destructive"><AlertDescription>
        {page.error}
        <Button variant="outline" size="sm" className="ml-3" onClick={() => void refresh()}>{t("app.retry")}</Button>
      </AlertDescription></Alert>}
      {page.loading && <Skeleton className="h-40" />}
      {empty && <p className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
        {t(mode === "pending" ? "discovery.empty_pending" : "discovery.empty_recent")}
      </p>}
      {page.data?.map((request) => (
        <EnrollmentRequestCard key={request.id} request={request} busy={false}
          onApprove={setApproving} onReject={setRejecting} />
      ))}
      {page.nextCursor && <Button variant="outline" disabled={page.loadingMore} onClick={() => void page.loadMore()}>
        {t("discovery.load_more")}
      </Button>}
      {approving && <EnrollmentApprovalDialog key={approving.id} request={approving} grant={grantFor(approving)}
        groups={groups.data ?? []} onClose={() => setApproving(null)}
        onApproved={() => decided(t("discovery.approved"))} />}
      {rejecting && <EnrollmentRejectDialog key={rejecting.id} request={rejecting} onClose={() => setRejecting(null)}
        onRejected={() => decided(t("discovery.rejected"))} />}
    </section>
  )
}
