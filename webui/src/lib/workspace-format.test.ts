import { afterEach, expect, it, vi } from "vitest"
import { formatWorkspaceValue, workspaceAxisTime, workspaceScale } from "@/lib/workspace-format"
import { downloadWorkspaceFile, workspaceCSV, workspaceJSON } from "@/lib/workspace-export"
import { workspaceQuery, workspaceResult } from "@/test/metric-workspace-fixtures"

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

it("formats base units and keeps one consistent axis scale", () => {
  expect(formatWorkspaceValue(1024, "bytes", { locale: "en" })).toBe("1 KiB")
  expect(formatWorkspaceValue(1024, "bytes_per_second", { locale: "en" })).toBe("1 KiB/s")
  expect(formatWorkspaceValue(50, "percent", { locale: "en" })).toBe("50%")
  expect(formatWorkspaceValue(-2, "number", { locale: "en" })).toBe("-2")
  expect(formatWorkspaceValue(null, "number")).toBe("—")
  expect(formatWorkspaceValue(Infinity, "number")).toBe("—")
  const scale = workspaceScale("bytes", 1024 * 1024)
  expect(formatWorkspaceValue(1024, "bytes", { locale: "en", scale, decimals: 4 })).toBe("0.001 MiB")
  for (const [value, text] of [[0.1, "100 ms"], [1, "1 s"], [60, "1 min"], [3600, "1 h"], [86400, "1 d"]] as const) expect(formatWorkspaceValue(value, "seconds", { locale: "en" })).toBe(text)
  expect(workspaceScale("bytes", Infinity)).toEqual({ divisor: 1, suffix: "B" })
  expect(workspaceScale("custom", 1).suffix).toBe("custom")
  expect(workspaceAxisTime(0, 1000, "en")).not.toBe("")
  expect(workspaceAxisTime(0, 86400000, "en")).toContain("Jan")
})

it("exports exact queries and null gaps and prevents spreadsheet formulas", () => {
  const result = workspaceResult()
  result.series[0].label = '=HYPERLINK("https://untrusted")'
  result.series[0].points[0].value = -2
  const csv = workspaceCSV(result)
  expect(csv).toContain(`"'=HYPERLINK(""https://untrusted"")"`)
  expect(csv).toContain(",-2\r\n")
  expect(csv.endsWith(",")).toBe(true)
  expect(JSON.parse(workspaceJSON(workspaceQuery(), result))).toEqual({ query: workspaceQuery(), result })
})

it("downloads a safe filename and releases its object URL", async () => {
  vi.useFakeTimers()
  const revokeObjectURL = vi.fn()
  vi.stubGlobal("URL", { createObjectURL: vi.fn(() => "blob:metrics"), revokeObjectURL })
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    expect(this.download).toBe("my_panel.csv")
  })
  downloadWorkspaceFile("my panel", "csv", "csv")
  expect(click).toHaveBeenCalledOnce()
  expect(document.querySelector("a[download]")).toBeNull()
  await vi.runAllTimersAsync()
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:metrics")
})
it("exports the stable GPU device dimension without changing scalar CSV headers", () => {
 const result = workspaceResult()
 const scalar = workspaceCSV(result)
 result.series[0].device_id = "a".repeat(32)
 const gpu = workspaceCSV(result)
 expect(scalar.split("\r\n")[0]).toBe("timestamp,series,node,metric,aggregation,unit,value")
 expect(gpu.split("\r\n")[0]).toBe("timestamp,series,node,device,metric,aggregation,unit,value")
 expect(gpu).toContain('"' + "a".repeat(32) + '"')
})
