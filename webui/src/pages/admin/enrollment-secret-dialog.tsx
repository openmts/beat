import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { AgentSetupCode } from "@/components/agent-setup-code"
import { useLocale } from "@/context/locale"
import { enrollmentCommand } from "./enrollment-command"
import type { EnrollmentGrantSecret } from "@/types/enrollment"

interface EnrollmentSecretDialogProps {
  value: EnrollmentGrantSecret
  created: boolean
  onClose: () => void
}

/** 秘密明文只在创建或轮换后出现一次；关闭即从界面状态里清掉。 */
export function EnrollmentSecretDialog({ value, created, onClose }: EnrollmentSecretDialogProps) {
  const { t } = useLocale()
  const command = enrollmentCommand(window.location.origin, value.secret)
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("discovery.secret_title")}</DialogTitle>
          <DialogDescription>{value.grant.name}</DialogDescription>
        </DialogHeader>
        <p className="text-sm leading-relaxed text-muted-foreground">
          {t(created ? "discovery.secret_created" : "discovery.secret_rotated")}
        </p>
        <AgentSetupCode title={t("discovery.command_title")} code={command} />
        <p className="text-xs leading-relaxed text-muted-foreground">{t("discovery.command_hint")}</p>
        <p className="text-xs leading-relaxed text-muted-foreground">{t("discovery.command_alternative")}</p>
        <AgentSetupCode title={t("discovery.secret_title")} code={value.secret} />
        <p className="text-xs leading-relaxed text-muted-foreground">{t("discovery.secret_hint")}</p>
        <DialogFooter>
          <Button onClick={onClose}>{t("app.close")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
