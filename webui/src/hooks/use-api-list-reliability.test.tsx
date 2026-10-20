import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import * as api from "@/lib/api"
import { useNodes } from "@/hooks/use-api"
import { APIError } from "@/lib/api-error"
import { workspaceNode } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/api")
beforeEach(() => vi.resetAllMocks())

it("preserves last data and reports a failed background refresh", async () => {
  vi.mocked(api.listNodes).mockResolvedValueOnce([workspaceNode()]).mockRejectedValueOnce(new Error("refresh unavailable"))
  const rendered = renderHook(() => useNodes())
  await waitFor(() => expect(rendered.result.current.data).toHaveLength(1))
  await act(() => rendered.result.current.refresh({ silent: true }))
  expect(rendered.result.current.data?.[0].id).toBe("node-one")
  expect(rendered.result.current.error).toBe("refresh unavailable")
})

it("ignores a late list response after the selected scope changes", async () => {
  let finishFirst!: (nodes: ReturnType<typeof workspaceNode>[]) => void
  vi.mocked(api.listNodes).mockImplementationOnce(() => new Promise((resolve) => { finishFirst = resolve }))
    .mockResolvedValueOnce([workspaceNode("new-scope")])
  const rendered = renderHook(({ group }) => useNodes(group), { initialProps: { group: "old" } })
  rendered.rerender({ group: "new" })
  await waitFor(() => expect(rendered.result.current.data?.[0].id).toBe("new-scope"))
  await act(async () => { finishFirst([workspaceNode("old-scope")]) })
  expect(rendered.result.current.data?.[0].id).toBe("new-scope")
})

it("reports the server retry delay so polling can back off", async () => {
  vi.mocked(api.listNodes).mockRejectedValue(new APIError("too many requests", 429, "7"))
  const rendered = renderHook(() => useNodes())
  await waitFor(() => expect(rendered.result.current.error).toBe("too many requests"))
  await expect(rendered.result.current.refresh({ silent: true })).resolves.toBe(7_000)
})

it("reports no delay for a successful refresh", async () => {
  vi.mocked(api.listNodes).mockResolvedValue([workspaceNode()])
  const rendered = renderHook(() => useNodes())
  await waitFor(() => expect(rendered.result.current.data).toHaveLength(1))
  await expect(rendered.result.current.refresh({ silent: true })).resolves.toBe(0)
})
