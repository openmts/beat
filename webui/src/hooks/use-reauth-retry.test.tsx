import { act, renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { APIError } from "@/lib/api-error"
import { useReauthRetry } from "@/hooks/use-reauth-retry"

const reauthenticate = vi.hoisted(() => vi.fn())
const principal = vi.hoisted(() => ({ value: { user: { totp_enabled_at: null } } }))
vi.mock("@/lib/security-api", () => ({ reauthenticateAdmin: reauthenticate }))
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: principal.value }) }))
vi.mock("@/context/locale", () => ({ useLocale: () => ({ t: (key: string) => key }), useOptionalLocale: () => ({ t: (key: string) => key }) }))
beforeEach(() => { reauthenticate.mockReset() })

describe("reauth retry", () => {
  it("runs the action directly when the server accepts it", async () => {
    const action = vi.fn(async () => "done")
    const hook = renderHook(() => useReauthRetry())
    await act(async () => { await hook.result.current.run(action) })
    expect(action).toHaveBeenCalledTimes(1)
    expect(hook.result.current.pending).toBe(false)
    expect(reauthenticate).not.toHaveBeenCalled()
  })

  it("defers a 428 action and replays it after the identity is confirmed", async () => {
    const action = vi.fn<() => Promise<string>>(async () => { throw new APIError("recent authentication is required", 428) })
    const hook = renderHook(() => useReauthRetry())
    await act(async () => { await hook.result.current.run(action) })
    await waitFor(() => expect(hook.result.current.pending).toBe(true))
    expect(reauthenticate).not.toHaveBeenCalled()
    action.mockImplementation(async () => "retried")
    reauthenticate.mockResolvedValue({ reauthenticated_until: "2030-01-01T00:00:00Z" })
    await act(async () => { await hook.result.current.confirm("secret", "") })
    expect(reauthenticate).toHaveBeenCalledWith("secret", "")
    expect(action).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(hook.result.current.pending).toBe(false))
  })

  it("keeps the caller's own error and never asks for a password", async () => {
    const hook = renderHook(() => useReauthRetry())
    await act(async () => {
      await expect(hook.result.current.run(async () => { throw new APIError("conflict", 409) }))
        .rejects.toThrow("conflict")
    })
    expect(hook.result.current.pending).toBe(false)
    expect(reauthenticate).not.toHaveBeenCalled()
  })

  it("reports a failed confirmation and keeps the deferred action", async () => {
    const hook = renderHook(() => useReauthRetry())
    await act(async () => { await hook.result.current.run(async () => { throw new APIError("recent authentication is required", 428) }) })
    reauthenticate.mockRejectedValue(new Error("invalid credentials"))
    await act(async () => { await hook.result.current.confirm("bad", "") })
    await waitFor(() => expect(hook.result.current.error).toBe("invalid credentials"))
    expect(hook.result.current.pending).toBe(true)
  })
})
