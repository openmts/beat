import { fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import type { MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"
import { workspaceResult } from "@/test/metric-workspace-fixtures"
import { WorkspaceStatsTable } from "./stats-view"

function series(patch: Partial<MetricResultSeries>): MetricResultSeries {
  return { ...workspaceResult().series[0], ...patch }
}

function twoSeries(): MetricQueryResult {
  const base = workspaceResult()
  return { ...base, series: [base.series[0], series({ ref_id: "B", metric: "memory", label: "Memory usage",
    points: [{ timestamp: Date.parse(base.from), value: 10 }] })] }
}

function draw(result: MetricQueryResult) {
  return render(<LocaleProvider><WorkspaceStatsTable result={result} title="CPU usage" /></LocaleProvider>)
}

/** URL 对象被替换成只保留导出所需的两个方法，避免 jsdom 真的去导航到 blob: 地址。 */
function stubDownload() {
  const created: Blob[] = []
  const createObjectURL = vi.fn((blob: Blob) => { created.push(blob); return "blob:stats" })
  vi.stubGlobal("URL", { createObjectURL, revokeObjectURL: vi.fn() })
  return { created, createObjectURL }
}

function rowLabels(): string[] {
  return Array.from(document.querySelectorAll("tbody tr")).map((row) => row.querySelector("td")?.textContent ?? "")
}

afterEach(() => vi.unstubAllGlobals())

it("renders one row per series plus the totals footer with coverage", () => {
  draw(workspaceResult())
  const rows = screen.getAllByRole("row")
  expect(rows).toHaveLength(3)
  const body = within(rows[1])
  expect(body.getByText("CPU usage")).toBeTruthy()
  // min/max/mean/first/last 都是同一个有效样本；空点只计入 Gaps。
  expect(body.getAllByText("40%")).toHaveLength(5)
  expect(body.getAllByText("1")).toHaveLength(2)
  expect(screen.getByText("Total · 1 series · 50% coverage")).toBeTruthy()
})

it("marks empty buckets and reports zero coverage when no bucket holds a valid sample", () => {
  draw({ ...workspaceResult(), series: [series({ points: [{ timestamp: 1, value: null }] })] })
  const gaps = document.querySelector("[data-gaps='1']")
  expect(gaps?.querySelector("span")?.className).toContain("text-destructive")
  expect(gaps?.getAttribute("title")).toContain("Empty buckets inside the query window")
  // 统计格没有有效样本时显示占位符，只有分桶存在但全空时覆盖率才如实报 0%。
  expect(screen.getAllByText("—")).toHaveLength(5)
  expect(screen.getByText("Total · 1 series · 0% coverage")).toBeTruthy()
})

it("reports the coverage placeholder when the series carries no bucket at all", () => {
  draw({ ...workspaceResult(), series: [series({ points: [] })] })
  const gaps = document.querySelector("[data-gaps='0']")
  expect(gaps?.querySelector("span")).toBeNull()
  expect(gaps?.getAttribute("title")).toBeNull()
  expect(screen.getAllByText("—")).toHaveLength(5)
  expect(screen.getByText("Total · 1 series · — coverage")).toBeTruthy()
})

it("sorts by a numeric column, flips the direction and keeps empty cells last", () => {
  const base = workspaceResult()
  const empty = series({ ref_id: "C", metric: "disk", label: "Disk usage", points: [{ timestamp: 1, value: null }] })
  draw({ ...base, series: [base.series[0], series({ ref_id: "B", metric: "memory", label: "Memory usage",
    points: [{ timestamp: 1, value: 10 }] }), empty] })
  expect(rowLabels()).toEqual(["CPU usage", "Memory usage", "Disk usage"])

  const maxHeader = screen.getByRole("columnheader", { name: /Max/ })
  fireEvent.click(within(maxHeader).getByRole("button"))
  expect(maxHeader.getAttribute("aria-sort")).toBe("ascending")
  expect(rowLabels()).toEqual(["Memory usage", "CPU usage", "Disk usage"])

  fireEvent.click(within(maxHeader).getByRole("button"))
  expect(maxHeader.getAttribute("aria-sort")).toBe("descending")
  expect(rowLabels()).toEqual(["CPU usage", "Memory usage", "Disk usage"])
  expect(screen.getByRole("columnheader", { name: /Series/ }).getAttribute("aria-sort")).toBe("none")
})

it("sorts by series label and exports the current order as CSV with localized headers", async () => {
  const { created, createObjectURL } = stubDownload()
  draw(twoSeries())
  const seriesHeader = screen.getByRole("columnheader", { name: /Series/ })
  fireEvent.click(within(seriesHeader).getByRole("button"))
  expect(rowLabels()).toEqual(["CPU usage", "Memory usage"])
  fireEvent.click(within(seriesHeader).getByRole("button"))
  expect(rowLabels()).toEqual(["Memory usage", "CPU usage"])

  fireEvent.click(screen.getByRole("button", { name: "Download stats CSV" }))
  expect(createObjectURL).toHaveBeenCalledTimes(1)
  const csv = (await created[0].text()).replace(/^\uFEFF/, "")
  const lines = csv.split("\r\n")
  expect(lines[0]).toBe('"Series","Min","Max","Mean","First","Last","Count","Gaps"')
  expect(lines[1].startsWith('"Memory usage"')).toBe(true)
  expect(lines[2].startsWith('"CPU usage"')).toBe(true)
})
