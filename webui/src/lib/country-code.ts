const COUNTRY_CODE = /^[A-Z]{2}$/

/** 只接受公开数据中的两位大写 ISO 3166-1 国家码。 */
export function normalizeCountryCode(value: unknown): string {
  return typeof value === "string" && COUNTRY_CODE.test(value) ? value : ""
}
