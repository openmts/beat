import { describe, expect, it } from "vitest"
import { gradientFill, gradientHueShift, gradientId, gradientStops, panelGradientMode, shiftHue } from "./workspace-gradient"

describe("workspace gradient stops", () => {
  it("falls back to none for missing or unknown modes", () => {
    expect(panelGradientMode(undefined)).toBe("none")
    expect(panelGradientMode("scheme")).toBe("none")
    expect(panelGradientMode("hue")).toBe("hue")
    expect(panelGradientMode("opacity")).toBe("opacity")
  })

  it("fades the fill out for the opacity mode and keeps the colour", () => {
    expect(gradientStops("#3b82f6", "opacity", 0.3)).toEqual([
      { offset: 0, color: "#3b82f6", opacity: 0.3 },
      { offset: 1, color: "#3b82f6", opacity: 0 },
    ])
    // none 走纯色，不生成第二个停靠点；这里按不透明填充返回同一份停靠点，供调用方复用。
    expect(gradientStops("#3b82f6", "none", 1)).toEqual([
      { offset: 0, color: "#3b82f6", opacity: 1 },
      { offset: 1, color: "#3b82f6", opacity: 0 },
    ])
  })

  it("shifts the hue without touching the opacity", () => {
    const stops = gradientStops("#3b82f6", "hue", 0.4)
    expect(stops[0]).toEqual({ offset: 0, color: "#3b82f6", opacity: 0.4 })
    expect(stops[1].opacity).toBe(0.4)
    expect(stops[1].color).not.toBe("#3b82f6")
    expect(stops[1].color).toMatch(/^#[0-9a-f]{6}$/)
  })

  it("keeps unparsable colours untouched and round-trips a rotation", () => {
    expect(shiftHue("var(--primary)", gradientHueShift)).toBe("var(--primary)")
    expect(shiftHue("#xyzxyz", gradientHueShift)).toBe("#xyzxyz")
    const shifted = shiftHue("#3b82f6", gradientHueShift)
    expect(channels(shiftHue(shifted, -gradientHueShift))).toEqual(channels("#3b82f6"))
  })

  it("builds SVG safe gradient identifiers", () => {
    expect(gradientId("panel 1/x", "A.1")).toBe("beat-gradient-panel-1-x-A-1")
    expect(gradientId("", "")).toBe("beat-gradient-x-x")
    expect(gradientFill("p", "A", "none", "#3b82f6")).toBe("#3b82f6")
    expect(gradientFill("p", "A", "hue", "#3b82f6")).toBe("url(#beat-gradient-p-A)")
  })
})

function channels(color: string): number[] {
  return [1, 3, 5].map((index) => Number.parseInt(color.slice(index, index + 2), 16))
}
