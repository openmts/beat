import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import SSHKeys from "@/pages/admin/ssh-keys"
import Alerts from "@/pages/admin/alerts"
import AlertRulesPanel from "@/pages/admin/alert-rules-panel"
import AlertChannelsPanel from "@/pages/admin/alert-channels-panel"
import AlertEventsPanel from "@/pages/admin/alert-events-panel"
import { isValidWebhookURL, messageFromError } from "@/pages/admin/alert-utils"
import * as hooks from "@/hooks/use-api"
import * as api from "@/lib/api"

vi.mock("@/hooks/use-api")
vi.mock("@/lib/api")
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog, DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough, DialogTitle: mocks.PassThrough,
    DialogDescription: mocks.PassThrough, DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect, SelectContent: mocks.PassThrough,
    SelectGroup: mocks.PassThrough, SelectItem: mocks.MockSelectItem,
    SelectTrigger: () => null, SelectValue: () => null,
  }
})
vi.mock("@/components/ui/tabs", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Tabs: mocks.MockTabs, TabsList: mocks.PassThrough,
    TabsTrigger: mocks.MockTabsTrigger, TabsContent: mocks.MockTabsContent,
  }
})
vi.mock("@/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange }: { checked: boolean; onCheckedChange: () => void }) => (
    <button role="switch" aria-checked={checked} onClick={onCheckedChange}>toggle</button>
  ),
}))

const state = (data: unknown, overrides = {}) => ({
  data, loading: false, error: null, refresh: vi.fn().mockResolvedValue(undefined), ...overrides,
})
const key = {
  id: "k1", name: "Managed", key_type: "ed25519", public_key: "ssh-ed25519 AAA",
  fingerprint: "SHA256:fp", created_at: "",
}
const rule = {
  id: "r1", name: "CPU high", description: "hot", metric: "cpu", operator: "gt",
  threshold: 80, duration: 300, severity: "critical", enabled: true,
  created_at: "", updated_at: "",
}
const channel = {
  id: "c1", name: "Primary webhook", channel_type: "webhook",
  config: `{"url":"https://example.com/hook"}`,
  enabled: true,
  last_delivery: { state: "success", message: "delivered", delivered_at: "2026-01-01T00:00:00Z" },
  created_at: "", updated_at: "",
}

function view(element: React.ReactNode) {
  return render(<LocaleProvider><MemoryRouter>{element}</MemoryRouter></LocaleProvider>)
}

describe("SSH keys", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(hooks.useSSHKeys).mockReturnValue(state([key]) as never)
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    })
  })

  it("renders states and key actions", async () => {
    vi.mocked(api.getSSHKey).mockResolvedValue({
      ...key,
      private_key: "-----BEGIN PRIVATE KEY-----\nprivate-content\n-----END PRIVATE KEY-----",
    } as never)
    view(<SSHKeys />)
    const rowButtons = within(screen.getByText("Managed").closest("tr")!).getAllByRole("button")
    fireEvent.click(rowButtons[0])
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(key.public_key))
    fireEvent.click(rowButtons[1])
    expect(await screen.findByText(/BEGIN PRIVATE KEY/)).toBeInTheDocument()
    expect(screen.getByText(key.public_key)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    fireEvent.click(rowButtons[2])
    const confirmDialog = screen.getAllByRole("dialog")[0]
    fireEvent.click(within(confirmDialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(api.deleteSSHKey).toHaveBeenCalledWith("k1"))
  })

  it("imports and generates keys with full key pair", async () => {
    const generated = {
      id: "k2", name: "Generated", key_type: "rsa",
      public_key: "ssh-rsa AAAAGeneratedPublic",
      private_key: "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----",
      fingerprint: "SHA256:gen", created_at: "",
    }
    vi.mocked(api.generateSSHKey).mockResolvedValue(generated as never)
    view(<SSHKeys />)
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    const dialog = screen.getByRole("dialog")
    const inputs = within(dialog).getAllByRole("textbox")
    fireEvent.change(inputs[0], { target: { value: " Imported " } })
    fireEvent.change(inputs[1], { target: { value: " ssh-rsa AAA " } })
    fireEvent.change(inputs[2], { target: { value: " private " } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.createSSHKey).toHaveBeenCalledWith({
      name: "Imported", public_key: "ssh-rsa AAA", private_key: "private", key_type: "imported",
    }))
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    fireEvent.click(screen.getByRole("button", { name: "Generate Key" }))
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "Generated" } })
    const keyTypeSelect = screen.getByTestId("select")
    expect(keyTypeSelect).toHaveAttribute("data-selected-label", "Ed25519")
    fireEvent.change(keyTypeSelect, { target: { value: "rsa" } })
    expect(keyTypeSelect).toHaveAttribute("data-selected-label", "RSA 2048")
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.generateSSHKey).toHaveBeenCalledWith("Generated", "rsa"))
    const generatedDialog = await screen.findByRole("dialog")
    expect(within(generatedDialog).getByText(generated.public_key)).toBeInTheDocument()
    expect(within(generatedDialog).getByText(/BEGIN RSA PRIVATE KEY/)).toBeInTheDocument()
    fireEvent.click(within(generatedDialog).getByRole("button", { name: "Public Key" }))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(generated.public_key))
    fireEvent.click(within(generatedDialog).getByRole("button", { name: /Private Key/ }))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(generated.private_key))
    expect(await within(generatedDialog).findByText("Copied")).toBeInTheDocument()
    fireEvent.click(within(generatedDialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByText(generated.public_key)).not.toBeInTheDocument())
  })

  it("renders fetch, loading, empty, and clipboard failures", async () => {
    vi.mocked(hooks.useSSHKeys).mockReturnValue(state([], { error: "fetch failed" }) as never)
    const { rerender } = view(<SSHKeys />)
    expect(screen.getByText("fetch failed")).toBeInTheDocument()
    vi.mocked(hooks.useSSHKeys).mockReturnValue(state([], { loading: true }) as never)
    rerender(<LocaleProvider><MemoryRouter><SSHKeys /></MemoryRouter></LocaleProvider>)
    expect(document.querySelectorAll("tbody tr")).toHaveLength(3)
    vi.mocked(hooks.useSSHKeys).mockReturnValue(state([key]) as never)
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error("copy failed"))
    rerender(<LocaleProvider><MemoryRouter><SSHKeys /></MemoryRouter></LocaleProvider>)
    fireEvent.click(within(screen.getByText("Managed").closest("tr")!).getAllByRole("button")[0])
    expect(await screen.findByText("copy failed")).toBeInTheDocument()
  })
})

describe("alerts", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(hooks.useAlertRules).mockReturnValue(state([rule]) as never)
    vi.mocked(hooks.useAlertChannels).mockReturnValue(state([channel]) as never)
    vi.mocked(hooks.useAlertEvents).mockReturnValue(state([{
      id: "e1", rule_id: "r1", node_id: "n1", message: "high", value: 91,
      status: "triggered", triggered_at: "2026-01-01T00:00:00Z", resolved_at: null,
    }, {
      id: "e2", rule_id: "r1", node_id: "missing", message: "ok", value: 10,
      status: "resolved", triggered_at: "2026-01-01T00:00:00Z", resolved_at: "2026-01-01T00:01:00Z",
    }]) as never)
    vi.mocked(hooks.useNodes).mockReturnValue(state([{
      id: "n1", name: "Node One", alias: "", group_id: "", host: "host", port: 22,
      status: "online", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
    }]) as never)
    vi.mocked(hooks.useManagedNodes).mockReturnValue(state([{
      id: "n1", name: "Node One", alias: "", group_id: "", host: "host", port: 22,
      status: "online", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
    }, {
      id: "missing", name: "Hidden Node", alias: "", group_id: "", host: "hidden", port: 22,
      status: "offline", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
    }]) as never)
  })

  it("switches top-level alert tabs", () => {
    view(<Alerts />)
    expect(screen.getByText("CPU high")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Alert Channels" }))
    expect(screen.getByText("Primary webhook")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Alert Events" }))
    expect(screen.getByText("Node One")).toBeInTheDocument()
  })

  it("creates, edits, toggles, and deletes rules", async () => {
    view(<AlertRulesPanel />)
    fireEvent.click(screen.getByRole("switch"))
    await waitFor(() => expect(api.updateAlertRule).toHaveBeenCalledWith("r1", expect.objectContaining({ enabled: false })))
    const rowButtons = within(screen.getByText("CPU high").closest("tr")!).getAllByRole("button")
    fireEvent.click(rowButtons[1])
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(api.deleteAlertRule).toHaveBeenCalledWith("r1"))
    fireEvent.click(rowButtons[0])
    const ruleSelects = screen.getAllByTestId("select")
    expect(ruleSelects[0]).toHaveAttribute("data-selected-label", "CPU")
    expect(ruleSelects[1]).toHaveAttribute("data-selected-label", "> (greater than)")
    expect(ruleSelects[2]).toHaveAttribute("data-selected-label", "Critical")
    fireEvent.change(ruleSelects[0], { target: { value: "traffic_usage_percent" } })
    expect(ruleSelects[0]).toHaveAttribute("data-selected-label", "Traffic quota usage")
    fireEvent.change(screen.getByDisplayValue("CPU high"), { target: { value: "CPU changed" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.updateAlertRule).toHaveBeenCalledWith("r1", expect.objectContaining({
      name: "CPU changed", metric: "traffic_usage_percent",
    })))
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    fireEvent.change(screen.getAllByRole("textbox")[0], { target: { value: "New rule" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.createAlertRule).toHaveBeenCalledWith(expect.objectContaining({ name: "New rule", enabled: true })))
  })

  it("configures availability rules with explicit timeout and debounce", async () => {
    view(<AlertRulesPanel />)
    const rowButtons = within(screen.getByText("CPU high").closest("tr")!).getAllByRole("button")
    fireEvent.click(rowButtons[0])
    const ruleSelects = screen.getAllByTestId("select")
    fireEvent.change(ruleSelects[0], { target: { value: "heartbeat_age_seconds" } })
    expect(ruleSelects[0]).toHaveAttribute("data-selected-label", "Node availability")
    expect(ruleSelects[1]).toBeDisabled()
    expect(screen.getByLabelText("Offline after (seconds)")).toHaveValue(90)
    expect(screen.getByLabelText("Debounce (seconds)")).toHaveValue(30)
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.updateAlertRule).toHaveBeenCalledWith("r1", expect.objectContaining({
      metric: "heartbeat_age_seconds", operator: "gt", threshold: 90, duration: 30,
    })))
  })

  it("validates and manages channels", async () => {
    view(<AlertChannelsPanel />)
    expect(screen.getByText("Delivered")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("switch"))
    await waitFor(() => expect(api.updateAlertChannel).toHaveBeenCalledWith("c1", expect.objectContaining({ enabled: false })))
    const row = within(screen.getByText("Primary webhook").closest("tr")!)
    fireEvent.click(row.getByRole("button", { name: "Test" }))
    await waitFor(() => expect(api.testAlertChannel).toHaveBeenCalledWith("c1"))
    fireEvent.click(row.getByRole("button", { name: "Edit" }))
    expect(screen.getByTestId("select")).toHaveAttribute("data-selected-label", "Webhook")
    fireEvent.change(screen.getByLabelText("Webhook URL"), { target: { value: "invalid" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("Enter a valid webhook URL")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Webhook URL"), { target: { value: "https://new.example/hook" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.updateAlertChannel).toHaveBeenCalledWith("c1", expect.objectContaining({
      config: `{"url":"https://new.example/hook"}`,
    })))
    fireEvent.click(row.getByRole("button", { name: "Delete" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(api.deleteAlertChannel).toHaveBeenCalledWith("c1"))
  })

  it("creates Telegram channels without exposing internal select values", async () => {
    view(<AlertChannelsPanel />)
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    const typeSelect = screen.getByTestId("select")
    expect(typeSelect).toHaveAttribute("data-selected-label", "Webhook")
    fireEvent.change(typeSelect, { target: { value: "telegram" } })
    expect(typeSelect).toHaveAttribute("data-selected-label", "Telegram")
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Telegram ops" } })
    fireEvent.change(screen.getByLabelText("Bot token"), { target: { value: "123:secret" } })
    fireEvent.change(screen.getByLabelText("Chat ID"), { target: { value: "42" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.createAlertChannel).toHaveBeenCalledWith(expect.objectContaining({
      name: "Telegram ops", channel_type: "telegram",
      config: `{"bot_token":"123:secret","chat_id":"42"}`,
    })))
  })

  it("shows readable email channel and SMTP security labels", () => {
    view(<AlertChannelsPanel />)
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    const typeSelect = screen.getByTestId("select")
    fireEvent.change(typeSelect, { target: { value: "email" } })
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "Email")
    expect(selects[1]).toHaveAttribute("data-selected-label", "STARTTLS")
    fireEvent.change(selects[1], { target: { value: "tls" } })
    expect(selects[1]).toHaveAttribute("data-selected-label", "TLS")
  })

  it("filters events and renders panel states", () => {
    view(<AlertEventsPanel />)
    expect(screen.getByText("Node One")).toBeInTheDocument()
    expect(screen.getByText("Hidden Node")).toBeInTheDocument()
    expect(within(screen.getByText("Node One").closest("tr")!).getByText("Triggered")).toBeInTheDocument()
    const statusSelect = screen.getByTestId("select")
    expect(statusSelect).toHaveAttribute("data-selected-label", "All")
    fireEvent.change(statusSelect, { target: { value: "resolved" } })
    expect(statusSelect).toHaveAttribute("data-selected-label", "Resolved")
    expect(screen.queryByText("Node One")).not.toBeInTheDocument()
    expect(screen.getByText("Hidden Node")).toBeInTheDocument()
  })

  it("covers alert errors, loading, empty, and utilities", () => {
    vi.mocked(hooks.useAlertRules).mockReturnValue(state([], { error: "rules failed" }) as never)
    expect(view(<AlertRulesPanel />).getByText("rules failed")).toBeInTheDocument()
    vi.mocked(hooks.useAlertChannels).mockReturnValue(state([], { loading: true }) as never)
    const { unmount } = view(<AlertChannelsPanel />)
    expect(document.querySelectorAll("tbody tr")).toHaveLength(2)
    unmount()
    vi.mocked(hooks.useAlertEvents).mockReturnValue(state([]) as never)
    expect(view(<AlertEventsPanel />).getByText("No data")).toBeInTheDocument()
    expect(isValidWebhookURL("https://example.com")).toBe(true)
    expect(isValidWebhookURL("ftp://example.com")).toBe(false)
    expect(isValidWebhookURL("bad")).toBe(false)
    expect(messageFromError(new Error("known"))).toBe("known")
    expect(messageFromError("unknown")).toBe("The request failed")
  })
})
