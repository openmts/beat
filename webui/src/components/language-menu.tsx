import { GlobeIcon, CheckIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useLocale } from "@/context/locale"
import { localeOptions, shippedLocales } from "@/context/locales"

// 语言菜单只列出目录完整的语言：原生名称 + 短码，当前语言带勾选图标。
// 选中后就地切换语言，不重新加载页面，也不改变导航或表单状态。
export function LanguageMenu({ size = "icon-sm" }: { size?: "icon-sm" | "icon" }) {
  const { locale, setLocale, t } = useLocale()
  const options = localeOptions.filter((option) => shippedLocales.includes(option.code))
  const current = options.find((option) => option.code === locale)
  const label = t("app.language")
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size={size}
            title={label}
            aria-label={`${label} · ${current?.nativeName ?? locale}`}
          />
        }
      >
        <GlobeIcon />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {options.map((option) => (
          <DropdownMenuItem
            key={option.code}
            aria-current={option.code === locale}
            onClick={() => setLocale(option.code)}
          >
            <span className="flex-1">{option.nativeName}</span>
            <span className="text-xs text-muted-foreground">{option.label}</span>
            {option.code === locale && <CheckIcon aria-hidden="true" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
