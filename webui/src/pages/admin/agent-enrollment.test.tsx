import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import AgentSetup from "@/pages/admin/agent-setup"
import * as enrollmentAPI from "@/lib/agent-enrollment-api"

vi.mock("@/lib/agent-enrollment-api")

const enrollment = {
  id: "join-one", token: "beat_join_v1_one", server_url: "https://beat.example.com",
  expires_at: new Date(Date.now() + 30 * 60_000).toISOString(),
  command: "sudo ./beat-agent join --server 'https://beat.example.com' --token 'beat_join_v1_one'",
}
const clipboard = vi.fn()

function view(locale = "en") {
  localStorage.setItem("locale", locale)
  return render(<LocaleProvider><MemoryRouter><AgentSetup /></MemoryRouter></LocaleProvider>)
}

function useHTTPS() {
  fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "https://beat.example.com" } })
}

describe("single-command agent enrollment", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(enrollmentAPI.createAgentEnrollmentToken).mockResolvedValue(enrollment)
    clipboard.mockResolvedValue(undefined)
    vi.stubGlobal("navigator", Object.assign(Object.create(navigator), { clipboard: { writeText: clipboard } }))
  })
  afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

  it("opens command generation by default without automatically issuing a token", () => {
    view()
    expect(screen.getByRole("tab", { name: "One-command setup" })).toHaveAttribute("aria-selected", "true")
    expect(screen.getByText(/already has the beat-agent executable/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Generate join command" })).toBeInTheDocument()
    expect(enrollmentAPI.createAgentEnrollmentToken).not.toHaveBeenCalled()
  })

  it("requires an explicit choice for HTTP and includes it in the request", async () => {
    view()
    expect(screen.getByRole("button", { name: "Generate join command" })).toBeDisabled()
    fireEvent.click(screen.getByRole("checkbox", { name: /Allow HTTP/ }))
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    await waitFor(() => expect(enrollmentAPI.createAgentEnrollmentToken).toHaveBeenCalledWith({
      server_url: window.location.origin, allow_http: true,
    }))
  })

  it("generates and copies the returned command, then requests a fresh token", async () => {
    view()
    useHTTPS()
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    expect(await screen.findByLabelText("Join command")).toHaveTextContent(enrollment.command)
    fireEvent.click(screen.getByRole("button", { name: "Copy Join command" }))
    await waitFor(() => expect(clipboard).toHaveBeenCalledWith(enrollment.command))
    const next = { ...enrollment, id: "join-two", token: "beat_join_v1_two", command: enrollment.command.replace("_one", "_two") }
    vi.mocked(enrollmentAPI.createAgentEnrollmentToken).mockResolvedValue(next)
    fireEvent.click(screen.getByRole("button", { name: "Generate another command" }))
    expect(await screen.findByLabelText("Join command")).toHaveTextContent(next.command)
    expect(screen.queryByText(enrollment.command)).not.toBeInTheDocument()
    expect(enrollmentAPI.createAgentEnrollmentToken).toHaveBeenCalledTimes(2)
    expect(localStorage.length).toBe(1)
    expect(sessionStorage.length).toBe(0)
  })

  it("clears a displayed credential when the server address changes", async () => {
    view()
    useHTTPS()
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    await screen.findByLabelText("Join command")
    fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "https://another.example.com" } })
    expect(screen.queryByText(enrollment.command)).not.toBeInTheDocument()
    useHTTPS()
    expect(screen.queryByText(enrollment.command)).not.toBeInTheDocument()
  })

  it("does not generate a command for an invalid origin", () => {
    view()
    fireEvent.change(screen.getByLabelText("Server URL"), { target: { value: "https://beat.example.com/admin" } })
    expect(screen.getByRole("button", { name: "Generate join command" })).toBeDisabled()
    expect(enrollmentAPI.createAgentEnrollmentToken).not.toHaveBeenCalled()
  })

  it("shows server and unexpected errors and supports retry", async () => {
    vi.mocked(enrollmentAPI.createAgentEnrollmentToken).mockRejectedValueOnce(new Error("rate limited"))
    view()
    useHTTPS()
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    expect(await screen.findByText("rate limited")).toBeInTheDocument()
    vi.mocked(enrollmentAPI.createAgentEnrollmentToken).mockRejectedValueOnce("unknown")
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    expect(await screen.findByText("Could not generate a join command. Try again.")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    expect(await screen.findByLabelText("Join command")).toBeInTheDocument()
  })

  it("prevents double submissions while a generation is pending", async () => {
    let resolve!: (value: typeof enrollment) => void
    vi.mocked(enrollmentAPI.createAgentEnrollmentToken).mockImplementation(() => new Promise(done => { resolve = done }))
    view()
    useHTTPS()
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    expect(screen.getByRole("button", { name: "Generating…" })).toBeDisabled()
    await act(async () => resolve(enrollment))
    expect(enrollmentAPI.createAgentEnrollmentToken).toHaveBeenCalledTimes(1)
  })

  it("removes expired commands and asks for a new one", async () => {
    vi.useFakeTimers()
    const expiresAt = new Date(Date.now() + 1_000).toISOString()
    vi.mocked(enrollmentAPI.createAgentEnrollmentToken).mockResolvedValue({ ...enrollment, expires_at: expiresAt })
    view()
    useHTTPS()
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Generate join command" })) })
    expect(screen.getByRole("button", { name: "Copy Join command" })).toBeInTheDocument()
    act(() => vi.advanceTimersByTime(1_000))
    expect(screen.getByText("This command has expired. Generate a new one.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Copy Join command" })).not.toBeInTheDocument()
  })

  it("keeps the current command while consulting configuration help", async () => {
    view()
    useHTTPS()
    fireEvent.click(screen.getByRole("button", { name: "Generate join command" }))
    await screen.findByLabelText("Join command")
    fireEvent.click(screen.getByRole("tab", { name: "Configuration fields" }))
    fireEvent.click(screen.getByRole("tab", { name: "One-command setup" }))
    expect(screen.getByLabelText("Join command")).toHaveTextContent(enrollment.command)
    expect(enrollmentAPI.createAgentEnrollmentToken).toHaveBeenCalledTimes(1)
  })

  it("renders the generation flow and local recovery help in Chinese", () => {
    view("zh-CN")
    expect(screen.getByRole("button", { name: "生成接入命令" })).toBeInTheDocument()
    expect(screen.getByText(/30 分钟/)).toBeInTheDocument()
    expect(screen.getByText(/service install --config/)).toBeInTheDocument()
  })
})
