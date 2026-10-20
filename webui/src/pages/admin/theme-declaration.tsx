import { useState } from "react"
import { ChevronDownIcon, ChevronRightIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { useThemeOperation } from "@/hooks/use-theme-operation"
import { configureTheme, previewTheme } from "@/lib/theme-api"
import { configurationFromDefinition } from "@/lib/theme-configuration-schema"
import { configurationSummary, declarationResources, hiddenSwitches, paletteEntries } from "@/lib/theme-declaration"
import { ThemeOperationDialog } from "@/pages/admin/theme-operation-dialog"
import { ThemeSettingsForm } from "@/pages/admin/theme-settings-form"
import type { ActiveThemeDescriptor, ThemePackage } from "@/types/theme"
import type { ThemeConfiguration } from "@/types/theme-definition"

/**
 * 主题包声明摘要：激活前先看清这个包声明了什么，并可就地改站点可配置项。
 * 数据来自既有的 `GET /admin/themes/{id}/preview`（只读、no-store），点开才请求，避免卡片列表发起 N 次请求；
 * 保存与重置复用预览弹窗同一条 `PUT /admin/themes/{id}/configuration` 路径（含最近重认证确认）。
 */
export function ThemeDeclaration({ theme }: { theme: ThemePackage }) {
  const { t } = useLocale()
  const [descriptor, setDescriptor] = useState<ActiveThemeDescriptor | null>(null)
  const [open, setOpen] = useState(false)
  const [failed, setFailed] = useState(false)
  const [draft, setDraft] = useState<ThemeConfiguration>({})
  const flow = useThemeOperation()
  function toggle() {
    setOpen(!open)
    if (open || descriptor || failed) return
    void previewTheme(theme.id).then(setDescriptor).catch(() => setFailed(true))
  }
  return <div className="space-y-3">
    <Button variant="ghost" size="sm" className="px-0" onClick={toggle} aria-expanded={open}
      aria-label={`${t("theme.declaration")} · ${theme.name}`}>
      {open ? <ChevronDownIcon /> : <ChevronRightIcon />}{t("theme.declaration")}</Button>
    {open && failed && <p role="alert" className="text-xs text-destructive">{t("theme.declaration_failed")}</p>}
    {open && descriptor && <DeclarationBody descriptor={descriptor} draft={draft} flow={flow} onDraft={setDraft} onDescriptor={setDescriptor} />}
  </div>
}

interface BodyProps {
  descriptor: ActiveThemeDescriptor
  draft: ThemeConfiguration
  flow: ReturnType<typeof useThemeOperation>
  onDraft: (value: ThemeConfiguration) => void
  onDescriptor: (value: ActiveThemeDescriptor) => void
}

function DeclarationBody({ descriptor, draft, flow, onDraft, onDescriptor }: BodyProps) {
  const { t } = useLocale()
  const definition = descriptor.declarative
  const schema = descriptor.configuration_schema
  // 编辑中的值覆盖当前生效定义，表单与摘要都读同一份草稿，与预览弹窗的口径一致。
  const edited = definition ? { ...definition, ...draft } : undefined
  // 内置主题没有包 ID，没有可写的配置行，因此保持只读摘要。
  const editable = Boolean(definition && descriptor.package_id)
  const save = (reset: boolean) => flow.request({
    label: t(reset ? "theme.reset" : "theme.save"), description: t(reset ? "theme.confirm_reset" : "theme.confirm_save"),
    run: async () => {
      const configured = await configureTheme(descriptor.package_id!, reset || !edited ? {} : configurationFromDefinition(edited, schema))
      onDescriptor(configured)
      onDraft({})
    },
  })
  return <div className="space-y-3 rounded-md border bg-muted/30 p-3 text-xs">
    <p className="flex flex-wrap items-center gap-2"><Badge variant="outline">{t(`theme.runtime_${descriptor.runtime}`)}</Badge>
      <span className="text-muted-foreground">{t("theme.protocol")} {descriptor.protocol}</span>
      {descriptor.sandbox_entry && <span className="text-muted-foreground">{descriptor.sandbox_entry}</span>}</p>
    {!definition && <p className="text-muted-foreground">{t("theme.declaration_sandboxed")}</p>}
    {definition && <>
      <p><span className="text-muted-foreground">{t("theme.font")}: </span>{t(`theme.font_${definition.font ?? "system"}`)}</p>
      <p><span className="text-muted-foreground">{t("theme.resources")}: </span>{declarationResources(definition).map((resource) => t(`node.${resource}`)).join(" · ")}</p>
      <p><span className="text-muted-foreground">{t("theme.hidden_content")}: </span>{hiddenSwitches(definition).map((key) => t(`theme.${key}`)).join(" · ") || t("theme.hidden_none")}</p>
      <p><span className="text-muted-foreground">{t("theme.configurable")}: </span>
        {configurationSummary(schema, t).join(" · ")}</p>
      {(["light", "dark"] as const).map((mode) => <div key={mode} className="space-y-1">
        <p className="text-muted-foreground">{t(`theme.palette_${mode}`)}</p>
        <div className="flex flex-wrap gap-1">{paletteEntries(definition, mode).map(([token, color]) =>
          <span key={token} title={`${token} ${color}`} aria-label={`${token} ${color}`} className="size-5 rounded border"
            style={{ backgroundColor: color }} />)}</div>
      </div>)}
      {editable && edited && <div className="text-sm"><ThemeSettingsForm definition={edited} schema={schema} disabled={flow.loading}
        onChange={(patch) => onDraft({ ...draft, ...patch })} onSave={() => save(false)} onReset={() => save(true)} />
        <ThemeOperationDialog flow={flow} /></div>}
    </>}
  </div>
}
