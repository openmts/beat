/** Intl 格式化统一入口。
 *
 * 设计契约：界面上的日期、时间、数字、百分比与字节/速率一律使用应用当前语言（`useLocale().locale`），
 * 而不是浏览器或操作系统语言；因此这里不接受缺省语言，调用方必须显式传入应用语言。
 * formatter 按语言与选项缓存，避免每次渲染重建 `Intl` 对象。
 */

const dateTimeOptions: Intl.DateTimeFormatOptions = {
  year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", second: "numeric",
}

const dateTimeCache = new Map<string, Intl.DateTimeFormat>()
const numberCache = new Map<string, Intl.NumberFormat>()

function cacheKey(locale: string | undefined, options: object): string {
  return `${locale ?? ""}|${JSON.stringify(options)}`
}

/** dateTimeFormatter 返回缓存的日期时间格式化器；缺省选项等于 `toLocaleString()` 的取值。 */
export function dateTimeFormatter(locale: string | undefined, options: Intl.DateTimeFormatOptions = dateTimeOptions): Intl.DateTimeFormat {
  const key = cacheKey(locale, options)
  const cached = dateTimeCache.get(key)
  if (cached) return cached
  const formatter = new Intl.DateTimeFormat(locale, options)
  dateTimeCache.set(key, formatter)
  return formatter
}

/** numberFormatter 返回缓存的数字格式化器。 */
export function numberFormatter(locale: string | undefined, options: Intl.NumberFormatOptions = {}): Intl.NumberFormat {
  const key = cacheKey(locale, options)
  const cached = numberCache.get(key)
  if (cached) return cached
  const formatter = new Intl.NumberFormat(locale, options)
  numberCache.set(key, formatter)
  return formatter
}

/** 无效或缺失的时间戳统一渲染为破折号，调用方不必各自判空。 */
function invalid(value: Date | number | string | null | undefined): boolean {
  if (value === null || value === undefined || value === "") return true
  return value instanceof Date ? Number.isNaN(value.getTime()) : !Number.isFinite(new Date(value).getTime())
}

/** formatDateTime 输出完整的日期与时间（含秒），语言由调用方给出。 */
export function formatDateTime(locale: string | undefined, value: Date | number | string | null | undefined): string {
  if (invalid(value)) return "—"
  return dateTimeFormatter(locale).format(new Date(value as number))
}

/** formatTime 输出小时与分钟，用于图表轴与紧凑列表。 */
export function formatTime(locale: string | undefined, value: Date | number | string | null | undefined): string {
  if (invalid(value)) return "—"
  return dateTimeFormatter(locale, { hour: "2-digit", minute: "2-digit" }).format(new Date(value as number))
}

/** formatDate 只输出日期，缺省与 `toLocaleDateString()` 的取值一致。 */
export function formatDate(locale: string | undefined, value: Date | number | string | null | undefined): string {
  if (invalid(value)) return "—"
  return dateTimeFormatter(locale, { year: "numeric", month: "numeric", day: "numeric" }).format(new Date(value as number))
}

/** formatTimeWithSeconds 输出到秒的时钟读数，用于运行状态与最近成功时间。 */
export function formatTimeWithSeconds(locale: string | undefined, value: Date | number | string | null | undefined): string {
  if (invalid(value)) return "—"
  return dateTimeFormatter(locale, { hour: "numeric", minute: "numeric", second: "numeric" }).format(new Date(value as number))
}

/** formatNumber 依据语言与小数位格式化数字；非有限值渲染为破折号。 */
export function formatNumber(locale: string | undefined, value: number | null | undefined, options: Intl.NumberFormatOptions = {}): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—"
  return numberFormatter(locale, options).format(value)
}

/** formatInteger 用于计数列：四舍五入后按语言插入千位分隔符。 */
export function formatInteger(locale: string | undefined, value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—"
  return numberFormatter(locale, { maximumFractionDigits: 0 }).format(Math.round(value))
}

/** formatDecimal 用于固定小数位的读数；缺省一位且不补零，保持既有展示。 */
export function formatDecimal(locale: string | undefined, value: number, digits = 1): string {
  return formatNumber(locale, value, { minimumFractionDigits: 0, maximumFractionDigits: digits })
}

/** formatPercent 接收以百分数为单位的数值（42.7 表示 42.7%），由 Intl 决定符号位置与写法。 */
export function formatPercent(locale: string | undefined, percent: number, digits = 1): string {
  if (!Number.isFinite(percent)) return "—"
  return numberFormatter(locale, { style: "percent", minimumFractionDigits: 0, maximumFractionDigits: digits }).format(percent / 100)
}

export type ByteUnit = "B" | "KiB" | "MiB" | "GiB" | "TiB" | "PiB"

export interface ByteFormatOptions {
  /** 小数位上限，缺省一位。 */
  decimals?: number
  /** 最大单位：每秒速率只展示到 MiB 一类场景由调用方显式收紧。 */
  maximumUnit?: ByteUnit
  /** 是否追加 `/s`，用于速率而不是容量。 */
  perSecond?: boolean
}

const byteUnits: ByteUnit[] = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]

/** formatBytes 以 1024 为进制缩放字节数，数字本身走 Intl，单位符号保持既有写法。 */
export function formatBytes(locale: string | undefined, value: number | null | undefined, options: ByteFormatOptions = {}): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—"
  const ceiling = byteUnits.indexOf(options.maximumUnit ?? "TiB")
  let index = 0
  while (Math.abs(value) >= 1024 ** (index + 1) && index < ceiling) index += 1
  const number = formatNumber(locale, value / 1024 ** index, { minimumFractionDigits: 0, maximumFractionDigits: options.decimals ?? 1 })
  return `${number} ${byteUnits[index]}${options.perSecond ? "/s" : ""}`
}
