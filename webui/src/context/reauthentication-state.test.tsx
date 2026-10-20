import MockAdapter from "axios-mock-adapter"
import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { AuthProvider, useAuth } from "@/context/auth"
import { api } from "@/lib/api-client"
import { reauthenticateAdmin } from "@/lib/security-api"

const mock = new MockAdapter(api)
const session = { id: "current-session", user_id: "owner", reauthenticated_until: null }
const renewed = "2099-01-01T00:00:00Z"

function Probe() {
  const auth = useAuth()
  return <><span>{auth.principal?.session.reauthenticated_until ?? "No recent proof"}</span>
    <span>{auth.loading ? "Loading" : "Ready"}</span>
    <button onClick={() => void reauthenticateAdmin("fixture password")}>Confirm identity</button></>
}

describe("shared recent authentication state", () => {
  beforeEach(() => {
    mock.onGet("/auth/state").reply(200, { setup_required: false })
    mock.onGet("/auth/session").reply(200, { user: { id: "owner", role: "owner" }, session })
  })
  afterEach(() => mock.reset())
  it("shares renewed proof across administration panels without remounting the application", async () => {
    mock.onPost("/auth/reauthenticate").reply(200, { ...session, reauthenticated_until: renewed })
    render(<AuthProvider><Probe /></AuthProvider>)
    await screen.findByText("Ready")
    fireEvent.click(screen.getByRole("button", { name: "Confirm identity" }))
    expect(await screen.findByText(renewed)).toBeInTheDocument()
    expect(screen.queryByText("Loading")).not.toBeInTheDocument()
    expect(mock.history.get.filter(request => request.url === "/auth/session")).toHaveLength(1)
  })
  it("ignores responses for a different session and unsubscribes on unmount", async () => {
    mock.onPost("/auth/reauthenticate").reply(200, { ...session, id: "older-session", reauthenticated_until: renewed })
    const view = render(<AuthProvider><Probe /></AuthProvider>)
    await screen.findByText("Ready")
    await act(async () => { await reauthenticateAdmin("fixture password") })
    expect(screen.getByText("No recent proof")).toBeInTheDocument()
    view.unmount()
    await act(async () => { await reauthenticateAdmin("fixture password") })
    expect(screen.queryByText(renewed)).not.toBeInTheDocument()
  })
})
