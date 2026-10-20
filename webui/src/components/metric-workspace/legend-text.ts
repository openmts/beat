import { useLocale } from "@/context/locale"

// 图例取值、排序与提示框的文案位于 metric-workspace 目录的 workspace.legend.* 键。
export type LegendTextKey = "series" | "values" | "last" | "min" | "max" | "mean" | "sum" | "count" | "sort" | "sort_none" | "sort_asc" | "sort_desc" | "tooltip" | "tooltip_all" | "tooltip_single" | "tooltip_off"

export function useLegendText(): (key: LegendTextKey) => string {
  const { t } = useLocale()
  return (key) => t(`workspace.legend.${key}`)
}
