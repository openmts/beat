const million = 1000000n
const maximumMicros = 9223372036854775807n
export function priceToMicros(value: string): string | null {
 if (!value.trim()) return null
 if (!/^\d+(\.\d{1,6})?$/.test(value.trim())) throw new Error("invalid_price")
 const [integer, fractional = ""] = value.trim().split(".")
 const micros = BigInt(integer) * million + BigInt(fractional.padEnd(6, "0"))
 if (micros > maximumMicros) throw new Error("invalid_price")
 return micros.toString()
}
export function microsToPrice(value: string | null | undefined): string {
 if (value == null) return ""
 if (!/^\d+$/.test(value) || BigInt(value) > maximumMicros) throw new Error("invalid_price")
 const integer = BigInt(value), fraction = (integer % million).toString().padStart(6, "0").replace(/0+$/, "")
 return `${integer / million}${fraction ? `.${fraction}` : ""}`
}
export function formatPublicPrice(value: string | undefined, currency: string | undefined, locale: string): string | null {
 if (value === undefined || !currency || !/^[A-Z]{3}$/.test(currency)) return null
 try {
  const decimal = microsToPrice(value), [whole, fraction] = decimal.split(".")
  const formatter = new Intl.NumberFormat(locale, { style: "currency", currency, minimumFractionDigits: 0, maximumFractionDigits: 6 })
  const parts = formatter.formatToParts(BigInt(whole)), mark = new Intl.NumberFormat(locale).formatToParts(1.1).find(part => part.type === "decimal")?.value ?? "."
  const last = parts.findLastIndex(part => part.type === "integer")
  if (fraction && last >= 0) parts.splice(last + 1, 0, { type: "literal", value: mark + fraction })
  return parts.map(part => part.value).join("")
 } catch { return null }
}
export function validCalendarDate(value: string): boolean {
 if (!value) return true
 if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
 const date = new Date(`${value}T00:00:00Z`)
 return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value
}
