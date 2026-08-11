import { Link } from "react-router"
import { SearchXIcon, ArrowLeftIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { buttonVariants } from "@/components/ui/button"

function NotFound() {
  const { t } = useLocale()
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 px-4 text-center">
      <SearchXIcon className="size-12 text-muted-foreground" aria-hidden />
      <h1 className="text-4xl font-bold tracking-tight">404</h1>
      <p className="max-w-sm text-muted-foreground">{t("app.not_found")}</p>
      <Link to="/" className={buttonVariants({ variant: "default", size: "default" })}>
        <ArrowLeftIcon data-icon="inline-start" />
        {t("app.dashboard")}
      </Link>
    </div>
  )
}

export default NotFound
