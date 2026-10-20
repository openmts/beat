const uuidByteLength = 16
const uuidVersionMask = 0x0f
const uuidVersion = 0x40
const uuidVariantMask = 0x3f
const uuidVariant = 0x80
const uuidSegments = [8, 4, 4, 4, 12] as const

/** 生成 UUID v4；randomUUID 在局域网 HTTP 的非安全上下文中不可用，使用 Web Crypto 字节实现兜底。 */
export function randomUUID(): string {
  const source = globalThis.crypto
  if (typeof source?.randomUUID === "function") return source.randomUUID()
  if (!source?.getRandomValues) throw new Error("secure random source is unavailable")
  const bytes = source.getRandomValues(new Uint8Array(uuidByteLength))
  bytes[6] = (bytes[6] & uuidVersionMask) | uuidVersion
  bytes[8] = (bytes[8] & uuidVariantMask) | uuidVariant
  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("")
  let offset = 0
  return uuidSegments.map((length) => {
    const segment = hex.slice(offset, offset + length)
    offset += length
    return segment
  }).join("-")
}
