import { formatDateTime } from "@/lib/intl-format"

export function evaluationTime(value: string | null | undefined, locale?: string): string {
  if (!value) return "—"
  return formatDateTime(locale, value)
}
