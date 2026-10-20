import { PlusIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { useThemeMarketActions } from "@/hooks/use-theme-market-actions"
import { useThemeMarkets } from "@/hooks/use-theme-markets"
import { ThemeMarketSourceCard } from "@/pages/admin/theme-market-source-card"
import { ThemeMarketSourceDialog } from "@/pages/admin/theme-market-source-dialog"
import { ThemeOperationDialog } from "@/pages/admin/theme-operation-dialog"

export function MarketSources({ onInstalled }: { onInstalled: () => void }) {
  const { t } = useLocale()
  const data = useThemeMarkets()
  const actions = useThemeMarketActions(data, onInstalled)
  return <><Card><CardHeader className="flex flex-wrap items-start justify-between gap-3">
    <div><CardTitle><h2>{t("theme.market_title")}</h2></CardTitle><CardDescription>{t("theme.market_description")}</CardDescription></div>
    <Button disabled={actions.flow.loading} onClick={() => actions.setEditor("new")}><PlusIcon />{t("theme.add_source")}</Button>
  </CardHeader><CardContent className="space-y-4">
    {data.error && <Alert variant="destructive"><AlertDescription>{data.error}</AlertDescription></Alert>}
    {actions.notice && <p role="status" className="text-sm">{actions.notice}</p>}
    {data.sources === null ? <p className="text-sm text-muted-foreground">{t("app.loading")}</p> :
      data.sources.length === 0 ? <p className="text-sm text-muted-foreground">{t("theme.no_sources")}</p> :
        data.sources.map((source) => <ThemeMarketSourceCard key={source.id} source={source} catalog={data.catalogs[source.id]}
          installed={data.installed} conflict={data.conflicts[source.id]} busy={actions.flow.loading} onAction={actions.sourceAction} onInstall={actions.install} />)}
  </CardContent></Card>
    {actions.editor && <ThemeMarketSourceDialog key={actions.editor === "new" ? "new" : actions.editor.id} source={actions.editor}
      open={actions.flow.operation === null} onClose={() => actions.setEditor(null)} onSave={actions.save} />}
    <ThemeOperationDialog flow={actions.flow} />
  </>
}
