import { useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"

interface AgentSetupCodeProps {
  title: string
  code: string
}

export function AgentSetupCode({ title, code }: AgentSetupCodeProps) {
  const { t } = useLocale()
  const [copiedText, setCopiedText] = useState<string | null>(null)
  const [errorText, setErrorText] = useState<string | null>(null)
  const copied = copiedText === code

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopiedText(code)
      setErrorText(null)
    } catch (_) {
      void _
      setCopiedText(null)
      setErrorText(code)
    }
  }

  return (
    <div className="min-w-0 overflow-hidden rounded-lg border">
      <div className="flex items-center justify-between gap-2 border-b bg-muted/50 px-3 py-2">
        <span className="truncate font-mono text-xs text-muted-foreground">{title}</span>
        <Button variant="ghost" size="xs" onClick={copy}
          aria-label={t(copied ? "setup.copied_label" : "setup.copy_label", { title })}>
          {copied ? <CheckIcon /> : <CopyIcon />}
          {t(copied ? "setup.copied" : "setup.copy")}
        </Button>
      </div>
      <pre tabIndex={0} aria-label={title} className="max-h-96 overflow-auto p-4 font-mono text-xs leading-relaxed whitespace-pre">
        <code>{code}</code>
      </pre>
      {errorText === code && (
        <p role="alert" className="border-t px-3 py-2 text-xs text-destructive">{t("setup.copy_failed")}</p>
      )}
    </div>
  )
}
