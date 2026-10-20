import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { countryName } from "@/lib/fleet-selectors"
import { formatPublicPrice } from "@/lib/lifecycle-format"
import { useFleetText } from "@/pages/fleet-text"
import type { Node } from "@/types"
export function PublicNodeMetadata({ node }: { node: Node }) {
 const { locale } = useLocale(), t = useFleetText(), location = node.location, lifecycle = node.lifecycle
 const country = countryName(location?.country_code, locale), price = formatPublicPrice(lifecycle?.price_micros, lifecycle?.currency, locale)
 if (!location && !lifecycle) return null
 return <div className="flex flex-wrap gap-1.5 text-xs">
  {location && <Badge variant="outline" title={location.resolved_at ?? undefined}>{country || (location.scope === "public" ? "—" : t("local"))}</Badge>}
  {lifecycle?.status && <Badge variant={lifecycle.status === "expired" ? "destructive" : "outline"}>{t(lifecycle.status)}{lifecycle.expires_on ? ` · ${lifecycle.expires_on}` : ""}</Badge>}
  {price !== null && <Badge variant="outline">{price}{lifecycle?.interval_unit ? ` / ${lifecycle.interval_count ?? 1} ${lifecycle.interval_unit}` : ""}</Badge>}
 </div>
}
