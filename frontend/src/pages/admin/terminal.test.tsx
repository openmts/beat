import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import TerminalPage from "@/pages/admin/terminal"
import * as hooks from "@/hooks/use-api"
import { executeBatchCommand } from "@/lib/api"

const terminal = vi.hoisted(() => ({
  dispose: vi.fn(), loadAddon: vi.fn(), open: vi.fn(), write: vi.fn(),
  onData: vi.fn(), fit: vi.fn(), sentData: undefined as ((data: string) => void) | undefined,
}))

vi.mock("@/hooks/use-api")
vi.mock("@/lib/api", () => ({ executeBatchCommand: vi.fn() }))
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    dispose = terminal.dispose
    loadAddon = terminal.loadAddon
    open = terminal.open
    write = terminal.write
    onData(callback: (data: string) => void) {
      terminal.sentData = callback
      terminal.onData(callback)
    }
  },
}))
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class { fit = terminal.fit },
}))
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect, SelectContent: mocks.PassThrough,
    SelectGroup: mocks.PassThrough, SelectItem: mocks.MockSelectItem,
    SelectTrigger: () => null, SelectValue: () => null,
  }
})

class MockWebSocket {
	static OPEN = 1
	url: string
	protocols: string[]
	readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  close = vi.fn()
  send = vi.fn()
	constructor(url: string, protocols: string[] = []) {
		this.url = url
		this.protocols = protocols
		sockets.push(this)
	}
}

const sockets: MockWebSocket[] = []
const onlineNode = {
  id: "n1", name: "Node One", alias: "Primary", group_id: "", host: "host", port: 22,
  status: "online", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
}
const offlineNode = { ...onlineNode, id: "n2", name: "Offline", status: "offline" }
const state = (data: unknown, overrides = {}) => ({
  data, loading: false, error: null, refresh: vi.fn(), ...overrides,
})

function view() {
  return render(<LocaleProvider><MemoryRouter><TerminalPage /></MemoryRouter></LocaleProvider>)
}

describe("admin terminal", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    sockets.length = 0
    terminal.sentData = undefined
    vi.mocked(hooks.useNodes).mockReturnValue(state([onlineNode, offlineNode]) as never)
    vi.stubGlobal("WebSocket", MockWebSocket)
  })

  it("renders fetch, loading, and no-online-node states", () => {
    vi.mocked(hooks.useNodes).mockReturnValue(state([], { error: "nodes failed" }) as never)
    const { rerender } = view()
    expect(screen.getByText("nodes failed")).toBeInTheDocument()
    vi.mocked(hooks.useNodes).mockReturnValue(state([], { loading: true }) as never)
    rerender(<LocaleProvider><MemoryRouter><TerminalPage /></MemoryRouter></LocaleProvider>)
    expect(document.querySelectorAll("[data-slot=skeleton]").length).toBeGreaterThan(0)
    vi.mocked(hooks.useNodes).mockReturnValue(state([]) as never)
    rerender(<LocaleProvider><MemoryRouter><TerminalPage /></MemoryRouter></LocaleProvider>)
    expect(screen.getByText("No online nodes are available")).toBeInTheDocument()
  })

  it("executes a batch command and reports per-node failures", async () => {
    vi.mocked(executeBatchCommand).mockResolvedValue([
      { node_id: "n1", node_name: "Node One", output: "up" },
      { node_id: "n2", error: "offline" },
    ])
    view()
    fireEvent.change(screen.getByPlaceholderText("e.g. uptime"), { target: { value: " uptime " } })
    fireEvent.click(screen.getByRole("button", { name: "Execute on all online nodes" }))
    await waitFor(() => expect(executeBatchCommand).toHaveBeenCalledWith(["n1"], " uptime "))
    expect(screen.getByText(/--- Node One ---/)).toHaveTextContent("up")
    expect(screen.getByText(/--- n2 ---/)).toHaveTextContent("Command failed: offline")
  })

  it("renders batch request errors", async () => {
    vi.mocked(executeBatchCommand).mockRejectedValue("failure")
    view()
    fireEvent.change(screen.getByPlaceholderText("e.g. uptime"), { target: { value: "uptime" } })
    fireEvent.click(screen.getByRole("button", { name: "Execute on all online nodes" }))
    expect(await screen.findByText("Error")).toBeInTheDocument()
  })

  it("bridges the WebSocket and terminal lifecycle", () => {
    const { unmount } = view()
    const select = screen.getByTestId("select")
    fireEvent.change(select, { target: { value: "n1" } })
    expect(select).toHaveAttribute("data-selected-label", "Primary (host)")
    fireEvent.click(screen.getByRole("button", { name: "Connect" }))
    expect(sockets).toHaveLength(1)
    expect(sockets[0].url).toContain("node_id=n1")
		expect(sockets[0].protocols).toEqual([])
    act(() => sockets[0].onopen?.())
    expect(screen.getByText("Connected")).toBeInTheDocument()
    act(() => sockets[0].onmessage?.({ data: "hello" }))
    expect(terminal.write).toHaveBeenCalledWith("hello")
    sockets[0].readyState = MockWebSocket.OPEN
    act(() => terminal.sentData?.("ls\n"))
    expect(sockets[0].send).toHaveBeenCalledWith("ls\n")
    act(() => window.dispatchEvent(new Event("resize")))
    expect(terminal.fit).toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }))
    expect(sockets[0].close).toHaveBeenCalled()
    act(() => sockets[0].onerror?.())
    expect(terminal.write).toHaveBeenCalledWith(expect.stringContaining("Connection error"))
    act(() => sockets[0].onclose?.())
    expect(terminal.write).toHaveBeenCalledWith(expect.stringContaining("Connection closed"))
    unmount()
    expect(terminal.dispose).toHaveBeenCalled()
  })
})
