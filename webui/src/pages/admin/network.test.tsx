import type { ReactNode } from "react"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/context/locale"
import * as apiHooks from "@/hooks/use-api"
import * as networkHooks from "@/hooks/use-network"
import * as networkApi from "@/lib/network-api"
import Network from "@/pages/admin/network"
import type { NetworkTaskPayload, NetworkTaskView } from "@/types"

vi.mock("@/hooks/use-api")
vi.mock("@/hooks/use-network")
vi.mock("@/lib/network-api")
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog,
    DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough,
    DialogTitle: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/network-task-dialog", () => ({
  NetworkTaskDialog: ({ open, view, onSave }: {
    open: boolean
    view: NetworkTaskView | null
    onSave: (payload: NetworkTaskPayload) => Promise<void>
  }) => open ? <button onClick={() => void onSave(taskPayload)}>{view ? "save-existing" : "save-new"}</button> : null,
}))
vi.mock("@/components/network-task-card", () => ({
  NetworkTaskCard: ({ view, onEdit, onDelete, onHistory, onMoveUp, onMoveDown }: {
    view: NetworkTaskView
    onEdit: () => void
    onDelete: () => void
    onHistory: () => void
    onMoveUp: () => void
    onMoveDown: () => void
  }) => (
    <article aria-label={view.task.name}>
      <span>{view.task.name}</span>
      <button onClick={onEdit}>edit</button>
      <button onClick={onDelete}>remove</button>
      <button onClick={onHistory}>history</button>
      <button onClick={onMoveUp}>up</button>
      <button onClick={onMoveDown}>down</button>
    </article>
  ),
}))
vi.mock("@/components/network-history-dialog", () => ({
  NetworkHistoryDialog: ({ open, view }: { open: boolean; view: NetworkTaskView | null }) => (
    open ? <div data-testid="history-dialog">{view?.task.name}</div> : null
  ),
}))

const refresh = vi.fn().mockResolvedValue(undefined)
const views = [taskView("one", "One", true, 0), taskView("two", "Two", true, 1), taskView("three", "Three", false, 2)]
const taskPayload: NetworkTaskPayload = {
  name: "Saved",
  type: "icmp",
  target: "example.com",
  ip_family: "auto",
  interval_seconds: 60,
  timeout_milliseconds: 3000,
  all_nodes: true,
  enabled: true,
  is_public: true,
  sort_order: 0,
  node_ids: [],
}

function state(data: unknown, overrides = {}) {
  return { data, loading: false, error: null, refresh, ...overrides }
}

function view(element: ReactNode) {
  return render(<LocaleProvider>{element}</LocaleProvider>)
}

describe("admin network page", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    refresh.mockResolvedValue(undefined)
    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state(views) as never)
    vi.mocked(apiHooks.useNodes).mockReturnValue(state([]) as never)
    vi.mocked(networkApi.createNetworkTask).mockResolvedValue({} as never)
    vi.mocked(networkApi.updateNetworkTask).mockResolvedValue({} as never)
    vi.mocked(networkApi.deleteNetworkTask).mockResolvedValue(undefined)
    vi.mocked(networkApi.sortNetworkTasks).mockResolvedValue(undefined)
  })

  it("creates, updates, deletes, sorts, and opens history", async () => {
    view(<Network />)
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    fireEvent.click(screen.getByRole("button", { name: "save-new" }))
    await waitFor(() => expect(networkApi.createNetworkTask).toHaveBeenCalledWith(taskPayload))

    const first = screen.getByRole("article", { name: "One" })
    fireEvent.click(within(first).getByRole("button", { name: "edit" }))
    fireEvent.click(screen.getByRole("button", { name: "save-existing" }))
    await waitFor(() => expect(networkApi.updateNetworkTask).toHaveBeenCalledWith("one", taskPayload))

    fireEvent.click(within(first).getByRole("button", { name: "history" }))
    expect(screen.getByTestId("history-dialog")).toHaveTextContent("One")
    fireEvent.click(within(first).getByRole("button", { name: "down" }))
    await waitFor(() => expect(networkApi.sortNetworkTasks).toHaveBeenCalledWith(["two", "one", "three"]))

    fireEvent.click(within(first).getByRole("button", { name: "remove" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(networkApi.deleteNetworkTask).toHaveBeenCalledWith("one"))
    expect(refresh).toHaveBeenCalled()
  })

  it("renders loading, empty, fetch errors, and action errors", async () => {
    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state([], { loading: true }) as never)
    const rendered = view(<Network />)
    expect(document.querySelectorAll("[data-slot=skeleton]")).toHaveLength(4)

    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state([]) as never)
    rendered.rerender(<LocaleProvider><Network /></LocaleProvider>)
    expect(screen.getByText("No network tasks")).toBeInTheDocument()

    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state(views, { error: "tasks failed" }) as never)
    rendered.rerender(<LocaleProvider><Network /></LocaleProvider>)
    expect(screen.getByText("tasks failed")).toBeInTheDocument()

    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state(views) as never)
    vi.mocked(networkApi.createNetworkTask).mockRejectedValueOnce(new Error("create failed"))
    rendered.rerender(<LocaleProvider><Network /></LocaleProvider>)
    fireEvent.click(screen.getByRole("button", { name: "Create task" }))
    fireEvent.click(screen.getByRole("button", { name: "save-new" }))
    expect(await screen.findAllByText("create failed")).not.toHaveLength(0)
  })
})

function taskView(id: string, name: string, enabled: boolean, sortOrder: number): NetworkTaskView {
  return {
    task: {
      id,
      name,
      type: "icmp",
      target: "example.com",
      ip_family: "auto",
      interval_seconds: 60,
      timeout_milliseconds: 3000,
      all_nodes: true,
      enabled,
      is_public: true,
      sort_order: sortOrder,
      nodes: [],
      created_at: "",
      updated_at: "",
    },
    nodes: [],
  }
}
