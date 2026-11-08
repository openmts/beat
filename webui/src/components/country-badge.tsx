import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { normalizeCountryCode } from "@/lib/country-code"
import { countryName } from "@/lib/fleet-selectors"
import { cn } from "@/lib/utils"

// 国家码 Badge 只做视觉提示，避免改变节点链接和其他控件的可访问名称。
export function CountryBadge({ code, className }: { code?: string; className?: string }) {
  const { locale } = useLocale()
  const normalized = normalizeCountryCode(code)
  if (!normalized) return null
  const name = countryName(normalized, locale) || normalized
  return (
    <Badge
      aria-hidden="true"
      data-country-code={normalized}
      title={name}
      variant="outline"
      className={cn("h-5 shrink-0 px-1.5 font-mono text-[10px] leading-none tracking-wider", className)}
    >
      {normalized}
    </Badge>
  )
}
