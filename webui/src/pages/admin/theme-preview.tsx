import { useEffect, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { PublicThemeProvider } from "@/components/public-theme-provider"
import { ThemePreviewViewport } from "@/components/theme-preview-viewport"
import { SandboxedThemeFrame } from "@/components/sandboxed-theme-frame"
import { useLocale } from "@/context/locale"
import { useThemeOperation } from "@/hooks/use-theme-operation"
import { useThemePreview } from "@/hooks/use-theme-preview"
import { configureTheme, previewTheme } from "@/lib/theme-api"
import { configurationFromDefinition } from "@/lib/theme-configuration-schema"
import { publicThemeStyles } from "@/lib/theme-presentation"
import { ThemeSettingsForm } from "@/pages/admin/theme-settings-form"
import { ThemePreviewContent } from "@/pages/admin/theme-preview-content"
import { ThemeOperationDialog } from "@/pages/admin/theme-operation-dialog"
import type { ActiveThemeDescriptor, ThemePackage } from "@/types/theme"
import type { ThemeConfiguration } from "@/types/theme-definition"

interface PreviewProps { theme: ThemePackage; onClose: () => void; onSaved: () => Promise<void> }
export function ThemePreview({ theme, onClose, onSaved }: PreviewProps) {
  const { t } = useLocale()
  const [descriptor, setDescriptor] = useState<ActiveThemeDescriptor | null>(null)
  const [error, setError] = useState("")
  useEffect(() => {
    let current = true
    void previewTheme(theme.id).then((value) => {
      if (value.declarative) { publicThemeStyles(value.declarative, "light"); publicThemeStyles(value.declarative, "dark") }
      if (current) setDescriptor(value)
    }).catch(() => { if (current) setError(t("theme.preview_failed")) })
    return () => { current = false }
  }, [theme.id, t])
  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent className="max-h-[95svh] overflow-auto sm:max-w-[min(1400px,95vw)]"><DialogHeader>
      <DialogTitle>{theme.name} · {t("theme.preview")}</DialogTitle><DialogDescription>{t("theme.preview_description")}</DialogDescription>
    </DialogHeader>{error ? <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert> : descriptor ?
      <PreviewWorkspace key={descriptor.configuration} descriptor={descriptor} onSaved={onSaved} onDescriptor={setDescriptor} /> : <p>{t("app.loading")}</p>}
    </DialogContent>
  </Dialog>
}

function PreviewWorkspace({ descriptor, onSaved, onDescriptor }: { descriptor: ActiveThemeDescriptor; onSaved: () => Promise<void>; onDescriptor: (value: ActiveThemeDescriptor) => void }) {
  const { t } = useLocale()
  const [mode, setMode] = useState<"light" | "dark">("light")
  const [mobile, setMobile] = useState(false)
  const [selected, setSelected] = useState<string>()
  const [draft, setDraft] = useState<ThemeConfiguration>({})
  const flow = useThemeOperation()
  const preview = useThemePreview(descriptor, mode, selected)
  const definition = descriptor.declarative ? { ...descriptor.declarative, ...draft } : undefined
  const save = (reset: boolean) => flow.request({ label: t(reset ? "theme.reset" : "theme.save"), description: t(reset ? "theme.confirm_reset" : "theme.confirm_save"), run: async () => {
    const configured = await configureTheme(descriptor.package_id!, reset ? {} :
      configurationFromDefinition(definition!, descriptor.configuration_schema))
    onDescriptor(configured)
    setDraft({})
    await onSaved()
  } })
  const navigate = (path: string) => setSelected(path === "/" ? undefined : path.slice("/node/".length))
  const fallback = <p className="p-6 text-muted-foreground">{t(preview.loading ? "app.loading" : "theme.preview_failed")}</p>
  return <div className="space-y-4"><PreviewControls mode={mode} mobile={mobile} onMode={setMode} onMobile={setMobile} />
    <div className={definition ? "grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_15rem]" : "min-w-0"}>
      <div className="min-w-0">{!preview.snapshot ? fallback : descriptor.runtime === "sandboxed" ?
        <div className="mx-auto max-w-full overflow-hidden rounded-lg border" style={{ width: mobile ? 390 : "100%" }}>
          <SandboxedThemeFrame descriptor={descriptor} snapshot={() => preview.snapshot!} fallback={fallback} onNavigate={navigate} /></div> : definition ?
        <ThemePreviewViewport title={`${descriptor.name} ${t("theme.preview_alt")}`} mobile={mobile}>
          <PublicThemeProvider definition={definition} snapshot={preview.snapshot} onNodeSelect={setSelected}><ThemePreviewContent onBack={() => setSelected(undefined)} /></PublicThemeProvider>
        </ThemePreviewViewport> : fallback}</div>
      {definition && <ThemeSettingsForm definition={definition} schema={descriptor.configuration_schema} disabled={flow.loading} onChange={(value) => setDraft((current) => ({ ...current, ...value }))} onSave={() => save(false)} onReset={() => save(true)} />}
    </div><ThemeOperationDialog flow={flow} /></div>
}

function PreviewControls({ mode, mobile, onMode, onMobile }: { mode: "light" | "dark"; mobile: boolean; onMode: (mode: "light" | "dark") => void; onMobile: (mobile: boolean) => void }) {
  const { t } = useLocale()
  return <div className="flex flex-wrap gap-3"><ToggleGroup value={[mobile ? "mobile" : "desktop"]} onValueChange={(value: string[]) => { if (value.length) onMobile(value[0] === "mobile") }}>
    <ToggleGroupItem value="desktop">{t("theme.desktop")}</ToggleGroupItem><ToggleGroupItem value="mobile">{t("theme.mobile")}</ToggleGroupItem>
  </ToggleGroup><ToggleGroup value={[mode]} onValueChange={(value: string[]) => { if (value.length) onMode(value[0] as "light" | "dark") }}>
    <ToggleGroupItem value="light">{t("theme.light")}</ToggleGroupItem><ToggleGroupItem value="dark">{t("theme.dark")}</ToggleGroupItem>
  </ToggleGroup></div>
}
