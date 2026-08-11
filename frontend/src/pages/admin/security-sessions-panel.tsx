import { useCallback, useEffect, useState } from "react"
import { LogOutIcon, XIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { listAdminSessions, revokeAdminSession, revokeOtherAdminSessions } from "@/lib/security-api"
import type { AdminSession } from "@/types/security"

export function SecuritySessionsPanel() {
  const { t } = useLocale()
  const [sessions, setSessions] = useState<AdminSession[]>([])
  const [error, setError] = useState("")

  const load = useCallback(async () => {
    try {
      setSessions(await listAdminSessions())
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }, [t])

  useEffect(() => { void load() }, [load])

  const runAction = async (action: () => Promise<void>) => {
    try {
      await action()
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  return (
    <div className="flex flex-col gap-4">
      {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      <div className="flex justify-end">
        <Button variant="outline" onClick={() => void runAction(async () => { await revokeOtherAdminSessions() })}>
          <LogOutIcon data-icon="inline-start" />{t("security.revoke_others")}
        </Button>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        {sessions.map((session) => (
          <Card key={session.id}>
            <CardHeader>
              <div className="flex items-center justify-between gap-2">
                <CardTitle className="truncate text-sm">{session.user_agent || session.ip_address}</CardTitle>
                {session.current && <Badge>{t("security.current_session")}</Badge>}
              </div>
              <CardDescription>{session.ip_address} · {session.token_prefix}</CardDescription>
            </CardHeader>
            <CardContent className="flex items-end justify-between gap-3">
              <div className="text-xs text-muted-foreground">
                <p>{t("security.last_activity")}: {new Date(session.last_activity_at).toLocaleString()}</p>
                <p>{t("security.expires")}: {new Date(session.absolute_expires_at).toLocaleString()}</p>
              </div>
              <Button variant="ghost" size="icon-sm" aria-label={t("security.revoke")}
                onClick={() => void runAction(() => revokeAdminSession(session.id))}>
                <XIcon />
              </Button>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
