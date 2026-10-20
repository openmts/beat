import { useLocale } from "@/context/locale"
import { prefixedTranslator } from "@/context/locales"

// 登录安全事件控制台的文案位于 security.events.*：与 security 目录同属管理端分片，
// 不会进入公共外壳（见 webui/scripts/public-shell-plugin.ts 的 250 KiB gzip 预算）。
export function useSecurityEventsText(): (key: string) => string {
  const { t } = useLocale()
  return prefixedTranslator(t, "security.events")
}

/** 词表标签按值本地化；目录缺项时先退回服务端标签，再退回机器值。 */
export function useSecurityLabel(): (group: string, value: string, fallback?: string) => string {
  const { t } = useLocale()
  return (group, value, fallback = "") => {
    if (!value) return fallback
    const key = `security.events.label.${group}.${value}`
    const label = t(key)
    return label === key ? fallback || value : label
  }
}
