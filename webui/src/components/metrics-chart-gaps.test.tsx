import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import type { ReactNode } from "react"
import { MetricsChart } from "@/components/metrics-chart"
import { LocaleProvider } from "@/context/locale"

vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <>{children}</>,
  LineChart: ({ children, data }: { children: ReactNode; data: unknown }) => <div><pre>{JSON.stringify(data)}</pre>{children}</div>,
  CartesianGrid: () => null, XAxis: () => null, YAxis: () => null,
  Tooltip: ({ formatter }: { formatter: (value: null) => string[] }) => <p>Missing value: {formatter(null)[0]}</p>,
  Line: ({ connectNulls }: { connectNulls: boolean }) => <div data-connect-nulls={connectNulls} />,
}))
describe("legacy preview chart nullable input", () => {
  it("preserves gaps rather than connecting samples or displaying zero", () => {
    render(<LocaleProvider><MetricsChart title="CPU" data={[{ timestamp: 1, value: 0 }, { timestamp: 2, value: null }, { timestamp: 3, value: 2 }]}
      color="blue" valueFormat="percent" from={1} to={3} rangeHours={1} /></LocaleProvider>)
    expect(screen.getByText(/timestamp.*null/)).toBeInTheDocument()
    expect(document.querySelector('[data-connect-nulls="false"]')).toBeInTheDocument()
    expect(screen.getByText("Missing value: --")).toBeInTheDocument()
  })
  it("reports no measurements for an entirely missing series", () => {
    render(<LocaleProvider><MetricsChart title="CPU" data={[{ timestamp: 1, value: null }]} color="blue" valueFormat="percent" from={1} to={3} rangeHours={1} /></LocaleProvider>)
    expect(screen.getByText("No measurements for this period")).toBeInTheDocument()
  })
})
