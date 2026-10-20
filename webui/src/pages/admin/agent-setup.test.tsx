import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import AgentSetup from "@/pages/admin/agent-setup"

function view(locale = "en") {
  localStorage.setItem("locale", locale)
  const result = render(<LocaleProvider><MemoryRouter><AgentSetup /></MemoryRouter></LocaleProvider>)
  fireEvent.click(screen.getByRole("tab", { name: locale === "zh-CN" ? "手动安装" : "Manual installation" }))
  return result
}

const clipboard = vi.fn()

describe("agent setup guide", () => {
  beforeEach(() => {
    clipboard.mockReset().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", Object.assign(Object.create(navigator), {
      clipboard: { writeText: clipboard },
    }))
  })

  afterEach(() => vi.unstubAllGlobals())

  it("guides operators through existing node creation and config-based enrollment", () => {
    view()
    expect(screen.getByRole("heading", { name: "Node setup" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Manage nodes" })).toHaveAttribute("href", "/admin/nodes")
    expect(screen.getByLabelText("Server URL")).toHaveValue(window.location.origin)
    expect(screen.getByText(/Save the token when creating/)).toBeInTheDocument()
    expect(screen.getByLabelText("agent.json")).toHaveTextContent("REPLACE_WITH_NODE_AGENT_TOKEN")
    expect(screen.getByLabelText("Monitored node · configure and start")).toHaveTextContent("--config /etc/beat-agent/agent.json")
    expect(screen.getByText(/Go 1.26.6/)).toBeInTheDocument()
    expect(screen.getByText(/localhost and 127\.0\.0\.1 point/)).toBeInTheDocument()
  })

  it("updates the JSON and connectivity command without persisting the server address", async () => {
    view()
    fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "https://monitor.example.com/" } })
    fireEvent.click(screen.getByRole("button", { name: "Copy agent.json" }))
    await waitFor(() => expect(clipboard).toHaveBeenCalledWith(expect.stringContaining('"server_url": "https://monitor.example.com"')))
    expect(await screen.findByRole("button", { name: "Copied agent.json" })).toBeInTheDocument()
    expect(localStorage.length).toBe(1)
    expect(sessionStorage.length).toBe(0)
    fireEvent.click(screen.getByRole("tab", { name: "Run & troubleshoot" }))
    expect(await screen.findByText(/curl --fail --show-error 'https:\/\/monitor.example.com\/readyz'/)).toBeInTheDocument()
  })

  it("does not offer runnable configuration for an invalid server URL", () => {
    view()
    fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "https://monitor.example.com/admin" } })
    expect(screen.getByLabelText("Server URL")).toHaveAttribute("aria-invalid", "true")
    expect(screen.getByText(/Enter an HTTP/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Copy agent.json" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("tab", { name: "Run & troubleshoot" }))
    expect(screen.queryByText(/curl --fail/)).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "http://192.168.1.48:8080" } })
    expect(screen.getByLabelText("Server URL")).toHaveAttribute("aria-invalid", "false")
  })

  it.each([
    ["Linux · ARM64 (aarch64)", "arm64"],
    ["Linux · x86_64 (amd64)", "amd64"],
  ])("selects %s and preserves it across guide tabs", async (option, architecture) => {
    const user = userEvent.setup()
    view()
    await user.click(screen.getByRole("combobox", { name: "Target architecture" }))
    await user.click(await screen.findByRole("option", { name: option }))
    expect(screen.getByLabelText("Build machine · repository root")).toHaveTextContent(`GOARCH=${architecture}`)
    fireEvent.click(screen.getByRole("tab", { name: "Configuration fields" }))
    fireEvent.click(screen.getByRole("tab", { name: "Manual installation" }))
    expect(screen.getByLabelText("Build machine · repository root")).toHaveTextContent(`GOARCH=${architecture}`)
    expect(screen.getByLabelText("Build machine · repository root")).toBeVisible()
  })

  it("documents all six fields and distinguishes template values from agent defaults", async () => {
    view()
    fireEvent.click(screen.getByRole("tab", { name: "Configuration fields" }))
    const table = await screen.findByRole("table", { name: "agent.json field reference" })
    for (const field of ["server_url", "agent_token", "node_name", "advertised_host", "ssh_port", "report_interval"]) {
      expect(within(table).getByText(field)).toBeInTheDocument()
    }
    const interval = within(table).getByText("report_interval").closest("tr")!
    expect(interval).toHaveTextContent("Required; no default")
    expect(interval).toHaveTextContent("1s")
    expect(interval).toHaveTextContent("5s")
    expect(within(table).getByText(/sshd_config/)).toBeInTheDocument()
    expect(screen.getByText(/Unknown fields/)).toBeInTheDocument()
    expect(screen.getByText(/Restart the agent/)).toBeInTheDocument()
  })

  it("documents every optional config section and its fields", async () => {
    view()
    fireEvent.click(screen.getByRole("tab", { name: "Configuration fields" }))
    expect(await screen.findByRole("heading", { name: "Optional sections" })).toBeInTheDocument()
    for (const section of ["updates", "accelerators", "operations", "operations.commands", "operations.file_roots"]) {
      expect(screen.getByRole("heading", { name: section })).toBeInTheDocument()
    }
    for (const field of ["node_id", "sample_interval_seconds", "max_concurrency", "working_root", "maximum_processes", "maximum_file_bytes"]) {
      expect(screen.getByText(field)).toBeInTheDocument()
    }
    const workingRoot = screen.getByText("working_root").closest("tr")!
    expect(workingRoot).toHaveTextContent("Required when commands are enabled")
    expect(workingRoot).toHaveTextContent("file_roots")
    expect(screen.getByText(/Signed auto-update/)).toBeInTheDocument()
    expect(screen.getByText(/require_cgroup is a runtime probe result/)).toBeInTheDocument()
    expect(screen.getByText(/File transfer allowlist as an array/)).toBeInTheDocument()
  })

  it("renders the optional config sections in Chinese", async () => {
    view("zh-CN")
    fireEvent.click(screen.getByRole("tab", { name: "配置字段" }))
    expect(await screen.findByRole("heading", { name: "可选配置段" })).toBeInTheDocument()
    expect(screen.getByText("operations.commands")).toBeInTheDocument()
    expect(screen.getByText(/命令的执行账户/)).toBeInTheDocument()
    expect(screen.getByText(/文件传输白名单/)).toBeInTheDocument()
  })

  it("provides systemd setup, heartbeat verification, and actionable failures", async () => {
    view()
    fireEvent.click(screen.getByRole("tab", { name: "Run & troubleshoot" }))
    expect(await screen.findByText(/UMask=0077/)).toHaveTextContent("User=root")
    expect(screen.getByText(/systemctl enable --now beat-agent/)).toBeInTheDocument()
    expect(screen.getByText(/journalctl -u beat-agent/)).toBeInTheDocument()
    expect(screen.getByText(/90 seconds/)).toBeInTheDocument()
    expect(screen.getByText(/401/)).toBeInTheDocument()
    expect(screen.getByText(/\/api\/v1\/agents\/ws/)).toBeInTheDocument()
    expect(screen.getByText(/Stop the foreground process/)).toBeInTheDocument()
  })

  it("explains clipboard failure and allows a retry", async () => {
    clipboard.mockRejectedValueOnce(new Error("denied"))
    view()
    fireEvent.click(screen.getByRole("button", { name: "Copy agent.json" }))
    expect(await screen.findByText(/Select and copy the text manually/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Copy agent.json" }))
    expect(await screen.findByRole("button", { name: "Copied agent.json" })).toBeInTheDocument()
    expect(screen.queryByText(/Select and copy the text manually/)).not.toBeInTheDocument()
  })

  it("handles browsers without a clipboard API", async () => {
    vi.stubGlobal("navigator", { clipboard: undefined })
    view()
    fireEvent.click(screen.getByRole("button", { name: "Copy agent.json" }))
    expect(await screen.findByText(/Select and copy the text manually/)).toBeInTheDocument()
  })

  it("resets the copied state when the generated example changes", async () => {
    view()
    fireEvent.click(screen.getByRole("button", { name: "Copy agent.json" }))
    await screen.findByRole("button", { name: "Copied agent.json" })
    fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "https://updated.example.com" } })
    expect(screen.getByRole("button", { name: "Copy agent.json" })).toBeInTheDocument()
  })

  it("renders the guide and reference in Chinese", async () => {
    view("zh-CN")
    expect(screen.getByRole("heading", { name: "节点接入" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "管理节点" })).toBeInTheDocument()
    expect(screen.getByText(/后台节点显示在线/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("tab", { name: "配置字段" }))
    expect(await screen.findByRole("table", { name: "agent.json 字段说明" })).toHaveTextContent("必填；无默认值")
    fireEvent.click(screen.getByRole("tab", { name: "常驻运行与排错" }))
    expect(await screen.findByText(/最低为 90 秒/)).toBeInTheDocument()
  })
})
