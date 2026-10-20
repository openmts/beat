import { useLocale } from "@/context/locale"
import { prefixedTranslator } from "@/context/locales"

// 文案位于目录 fleet.*；调用点键名保持不变，目录缺少该键时原样返回（错误信息透传）。
export function useFleetText() {
  const { t } = useLocale()
  return prefixedTranslator(t, "fleet")
}
