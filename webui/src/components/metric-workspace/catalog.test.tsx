import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { WorkspaceCatalog } from "./catalog"
import { APIError } from "@/lib/api-error"
import { updateMetricPolicy } from "@/lib/metric-workspace-api"
import { reauthenticateAdmin } from "@/lib/security-api"
import { workspaceSources } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/metric-workspace-api", () => ({ updateMetricPolicy: vi.fn() }))
vi.mock("@/lib/security-api", () => ({ reauthenticateAdmin: vi.fn() }))
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { role: "owner", totp_enabled_at: "enabled" } } }) }))
beforeEach(() => { vi.resetAllMocks() })

it("preserves inherited retention and retries a policy update only after identity proof", async () => {
  const user = userEvent.setup()
  const changed = vi.fn()
  vi.mocked(updateMetricPolicy).mockRejectedValueOnce(new APIError("proof required", 428)).mockResolvedValue(workspaceSources().catalog.metrics[0])
  vi.mocked(reauthenticateAdmin).mockResolvedValue({ reauthenticated_until: "2099-01-01T00:00:00Z" } as Awaited<ReturnType<typeof reauthenticateAdmin>>)
  render(<LocaleProvider><WorkspaceCatalog catalog={workspaceSources().catalog} changed={changed} close={vi.fn()} /></LocaleProvider>)
  fireEvent.change(screen.getByRole("textbox", { name: "Search metrics" }), { target: { value: "cpu" } })
  expect(screen.queryByText("Memory usage")).not.toBeInTheDocument()
  await user.click(screen.getByRole("button", { name: "Edit policy" }))
  fireEvent.change(screen.getByLabelText("Display label"), { target: { value: "Processor load" } })
  fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Compute saturation" } })
  await user.click(screen.getByRole("switch", { name: "Public" }))
  await user.click(screen.getByRole("button", { name: "Apply" }))
  await screen.findByRole("heading", { name: "Confirm shared dashboard changes" })
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "fixture-password" } })
  fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
  await user.click(screen.getByRole("button", { name: "Confirm and save" }))
  expect(updateMetricPolicy).toHaveBeenLastCalledWith(expect.objectContaining({ retention_override_days: null, public: false, label: "Processor load" }))
  expect(changed).toHaveBeenCalledOnce()
})
