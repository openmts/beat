import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { PublicNetworkHistory } from "@/components/public-network-history"

// 分片加载占位走文案目录，这里给 useLocale 一个稳定桩（与其它组件测试一致）。
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  const value = testLocaleValue("en")
  return { useLocale: () => value, useOptionalLocale: () => value }
})

const dialogChunkDelayMs = 20

vi.mock("@/components/network-history-dialog", async () => {
 await new Promise(resolve => setTimeout(resolve, dialogChunkDelayMs))
 return { NetworkHistoryDialog: ({ open }: { open: boolean }) => <div data-testid="history-dialog">{open ? "open" : "closed"}</div> }
})

describe("public network history chunk", () => {
 it("renders the lazily loaded dialog chunk for both consumers", async () => {
  const first = render(<PublicNetworkHistory open view={null} onOpenChange={vi.fn()} />)
  expect(await screen.findByTestId("history-dialog")).toHaveTextContent("open")
  first.unmount()
  render(<PublicNetworkHistory open={false} view={null} onOpenChange={vi.fn()} />)
  expect(await screen.findByTestId("history-dialog")).toHaveTextContent("closed")
 })
})
