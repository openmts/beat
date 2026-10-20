import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"

export function ThemeCleanupNotices({ pending, disabled, onRetry }: { pending: string[]; disabled: boolean; onRetry: (id: string) => void }) {
  const { t } = useLocale()
  return pending.map((id) => <Alert key={id}><AlertDescription className="space-y-2"><p>{t("theme.cleanup_pending")}</p>
    <Button variant="outline" size="sm" disabled={disabled} onClick={() => onRetry(id)}>{t("theme.retry_cleanup")}</Button>
  </AlertDescription></Alert>)
}
