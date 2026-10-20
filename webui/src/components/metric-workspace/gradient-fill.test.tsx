import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { WorkspaceGradientDefs } from "./gradient-fill"

function stops(props: Parameters<typeof WorkspaceGradientDefs>[0]) {
  const { container } = render(<svg><WorkspaceGradientDefs {...props} /></svg>)
  return [...container.querySelectorAll("stop")].map((stop) => [stop.getAttribute("stop-color"), stop.getAttribute("stop-opacity")])
}

describe("workspace gradient definitions", () => {
  it("renders nothing without a gradient", () => {
    const { container } = render(<svg><WorkspaceGradientDefs panelId="p" mode="none" opacity={0.3} direction="down" series={[]} /></svg>)
    expect(container.querySelector("defs")).toBeNull()
  })

  it("keeps the colour for opacity mode and rotates it for hue mode", () => {
    expect(stops({ panelId: "p", mode: "opacity", opacity: 0.3, direction: "down", series: [{ ref_id: "A", color: "#3b82f6" }] }))
      .toEqual([["#3b82f6", "0.3"], ["#3b82f6", "0"]])
    const [first, second] = stops({ panelId: "p", mode: "hue", opacity: 0.3, direction: "up", series: [{ ref_id: "A", color: "#3b82f6" }] })
    expect(first).toEqual(["#3b82f6", "0.3"])
    expect(second[1]).toBe("0.3")
    expect(second[0]).not.toBe("#3b82f6")
  })

  it("uses the per series opacity when a series carries one", () => {
    expect(stops({ panelId: "p", mode: "opacity", opacity: 0.9, direction: "down",
      series: [{ ref_id: "A", color: "#3b82f6", opacity: 0.2 }, { ref_id: "B", color: "#16a34a" }] }))
      .toEqual([["#3b82f6", "0.2"], ["#3b82f6", "0"], ["#16a34a", "0.9"], ["#16a34a", "0"]])
  })

  it("declares one gradient per series in the requested direction", () => {
    const { container } = render(<svg><WorkspaceGradientDefs panelId="panel 1" mode="opacity" opacity={0.5} direction="up"
      series={[{ ref_id: "A", color: "#3b82f6" }, { ref_id: "B", color: "#16a34a" }]} /></svg>)
    expect(container.querySelectorAll("linearGradient")).toHaveLength(2)
    expect(container.querySelector("#beat-gradient-panel-1-A")?.getAttribute("y1")).toBe("1")
    expect(container.querySelector("#beat-gradient-panel-1-A")?.getAttribute("y2")).toBe("0")
    expect(container.querySelector("#beat-gradient-panel-1-B")?.getAttribute("y1")).toBe("1")
  })
})
