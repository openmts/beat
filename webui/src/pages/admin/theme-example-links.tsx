import { BookOpenIcon, DownloadIcon, PlusIcon } from "lucide-react"
import { Button, buttonVariants } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { examplePreviewURL, themeExamples } from "@/lib/theme-examples"

const authorGuideURL = "/themes/theme-author-guide.md"

export function ThemeExampleLinks({ disabled, onInstall }: { disabled: boolean; onInstall: (id: string) => void }) {
  const { t } = useLocale()
  return <section className="space-y-3 rounded-xl border bg-muted/20 p-4">
    <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="text-sm font-semibold">{t("theme.examples")}</h3>
      <a href={authorGuideURL} download="theme-author-guide.md" className={buttonVariants({ variant: "ghost", size: "sm" })}><BookOpenIcon />{t("theme.author_guide")}</a></div>
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">{themeExamples.map((example) => <div key={example.id} className="space-y-3 overflow-hidden rounded-lg border bg-card p-3">
      {examplePreviewURL(example.id) && <img src={examplePreviewURL(example.id)!} alt={`${example.name} ${t("theme.preview_alt")}`} loading="lazy"
        className="aspect-video w-full rounded-md border object-cover object-top" />}
      <div><h4 className="text-sm font-semibold">{example.name}</h4><p className="mt-1 text-xs text-muted-foreground">{t(`theme.example_${example.id}`)}</p></div>
      <div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" disabled={disabled} onClick={() => onInstall(example.id)}><PlusIcon />{t("theme.install")}</Button>
        <a href={`/themes/${example.id}.theme.zip`} download className={buttonVariants({ variant: "ghost", size: "icon-sm" })} aria-label={`${t("theme.download")} ${example.name} ZIP`}><DownloadIcon /></a></div>
    </div>)}</div>
  </section>
}
