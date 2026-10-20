import { gradientModes, type GradientMode } from "@/lib/workspace-line-style"

/** hue 模式旋转的色相角度：够近以保持同族配色，够远才能在堆叠里分出层次。 */
export const gradientHueShift = 40

/** 渐变停靠点：offset 是 0..1 的相对位置，opacity 是 0..1 的不透明度。 */
export interface GradientStop { offset: number; color: string; opacity: number }

/** panelGradientMode 返回生效的渐变模式，未知取值与缺省一致按 none 处理。 */
export function panelGradientMode(value: string | undefined): GradientMode {
  return gradientModes.find((mode) => mode === value) ?? "none"
}

/** gradientStops 生成自上而下的两个停靠点：opacity 模式保留色相、只衰减透明度；
 *  hue 模式在保持亮度与透明度的同时旋转色相，让相邻序列在堆叠里能分开。
 *  传入的 opacity 是 0..1 的填充不透明度，与服务端 fill_opacity 百分比除以 100 后一致。 */
export function gradientStops(color: string, mode: GradientMode, opacity: number): GradientStop[] {
  if (mode === "hue") return [{ offset: 0, color, opacity }, { offset: 1, color: shiftHue(color, gradientHueShift), opacity }]
  return [{ offset: 0, color, opacity }, { offset: 1, color, opacity: 0 }]
}

/** gradientFill 返回渲染层的 fill 取值：none 模式用纯色，其它模式引用该序列的渐变。 */
export function gradientFill(panelId: string, refId: string, mode: GradientMode, color: string): string {
  return mode === "none" ? color : `url(#${gradientId(panelId, refId)})`
}

/** gradientId 必须是合法的 SVG 片段标识符：面板与序列 id 可能带其它字符，因此只保留
 *  [A-Za-z0-9_-]，其余替换成 `-`；替换后为空时回退成占位符，避免生成裸 id。 */
export function gradientId(panelId: string, refId: string): string {
  return `beat-gradient-${identifier(panelId)}-${identifier(refId)}`
}

function identifier(value: string): string {
  const cleaned = value.replace(/[^A-Za-z0-9_-]/g, "-")
  return cleaned === "" ? "x" : cleaned
}

interface Hsl { hue: number; saturation: number; lightness: number }

/** shiftHue 在 HSL 空间旋转色相后转回 `#rrggbb`；输入不是 6 位十六进制时原样返回，
 *  这样自定义色值与主题变量不会因为解析失败而变成黑色。 */
export function shiftHue(color: string, degrees: number): string {
  const rgb = parseHex(color)
  if (!rgb) return color
  const hsl = toHsl(rgb)
  return toHex({ hue: (hsl.hue + degrees + 360) % 360, saturation: hsl.saturation, lightness: hsl.lightness })
}

function parseHex(color: string): [number, number, number] | null {
  if (!/^#[0-9a-fA-F]{6}$/.test(color)) return null
  return [Number.parseInt(color.slice(1, 3), 16), Number.parseInt(color.slice(3, 5), 16), Number.parseInt(color.slice(5, 7), 16)]
}

function toHsl([red, green, blue]: [number, number, number]): Hsl {
  const [r, g, b] = [red / 255, green / 255, blue / 255]
  const maximum = Math.max(r, g, b), minimum = Math.min(r, g, b)
  const lightness = (maximum + minimum) / 2
  const delta = maximum - minimum
  if (delta === 0) return { hue: 0, saturation: 0, lightness }
  const saturation = delta / (1 - Math.abs(2 * lightness - 1))
  const segment = maximum === r ? ((g - b) / delta + 6) % 6 : maximum === g ? (b - r) / delta + 2 : (r - g) / delta + 4
  return { hue: (segment * 60) % 360, saturation, lightness }
}

function toHex(hsl: Hsl): string {
  const chroma = (1 - Math.abs(2 * hsl.lightness - 1)) * hsl.saturation
  const secondary = chroma * (1 - Math.abs(((hsl.hue / 60) % 2) - 1))
  const base = hsl.lightness - chroma / 2
  const sector = Math.floor(hsl.hue / 60) % 6
  const offsets: [number, number, number][] = [[chroma, secondary, 0], [secondary, chroma, 0], [0, chroma, secondary],
    [0, secondary, chroma], [secondary, 0, chroma], [chroma, 0, secondary]]
  const channels = offsets[sector].map((value) => Math.round((value + base) * 255))
  return `#${channels.map((value) => value.toString(16).padStart(2, "0")).join("")}`
}
