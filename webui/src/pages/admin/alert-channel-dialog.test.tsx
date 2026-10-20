import { useState } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import AlertChannelDialog from "@/pages/admin/alert-channel-dialog"
import { alertChannelFormFrom, emptyAlertChannelForm, type AlertChannelFormState } from "@/pages/admin/alert-channel-config"
import { notificationChannel } from "@/test/notification-fixtures"
import { notificationChannelTypes } from "@/lib/notification-fields"

vi.mock("@/components/ui/dialog", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Dialog: m.MockDialog, DialogContent: m.PassThrough, DialogHeader: m.PassThrough, DialogTitle: m.PassThrough, DialogDescription: m.PassThrough, DialogFooter: m.PassThrough }
})
vi.mock("@/components/ui/select", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Select: m.MockSelect, SelectContent: m.PassThrough, SelectGroup: m.PassThrough, SelectItem: m.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null }
})

function Harness({ initial = emptyAlertChannelForm(), editing = false, loading = false, save = vi.fn(), close = vi.fn() }: { initial?: AlertChannelFormState; editing?: boolean; loading?: boolean; save?: (form: AlertChannelFormState) => void; close?: (open: boolean) => void }) {
  const [form, setForm] = useState(initial)
  return <LocaleProvider><AlertChannelDialog open editing={editing} loading={loading} form={form} setForm={setForm} onOpenChange={close} onSave={() => save(form)} /></LocaleProvider>
}

describe("notification channel editor", () => {
  beforeEach(() => localStorage.setItem("locale", "en"))
  it("renders all supported providers using readable labels", () => {
    const expected = ["Webhook", "Telegram", "Email", "Bark", "ServerChan Turbo", "ServerChan 3", "Custom HTTP"]
    for (const [index, type] of notificationChannelTypes.entries()) {
      const view = render(<Harness initial={emptyAlertChannelForm(type)} />)
      expect(screen.getAllByTestId("select")[0]).toHaveAttribute("data-selected-label", expected[index])
      expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
      view.unmount()
    }
  })
  it("updates provider fields and subscriptions without submitting IDs as labels", () => {
    const save = vi.fn()
    render(<Harness save={save} />)
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "SMTP operations" } })
    fireEvent.change(screen.getAllByTestId("select")[0], { target: { value: "email" } })
    fireEvent.change(screen.getByLabelText(/^SMTP host/), { target: { value: "smtp.example.test" } })
    fireEvent.change(screen.getByLabelText(/^Port/), { target: { value: "465" } })
    fireEvent.change(screen.getAllByTestId("select")[1], { target: { value: "tls" } })
    fireEvent.change(screen.getByLabelText(/^Sender address/), { target: { value: "ops@example.test" } })
    fireEvent.change(screen.getByLabelText(/^Recipients/), { target: { value: "team@example.test" } })
    fireEvent.click(screen.getByRole("checkbox", { name: "Alert resolved" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ name: "SMTP operations", channelType: "email", values: expect.objectContaining({ host: "smtp.example.test", security: "tls", port: "465", to: "team@example.test" }) }))
    expect(save.mock.calls[0][0].eventKinds).not.toContain("alert.resolved")
  })
  it("retains saved credentials and explicitly clears parent or individual secrets", () => {
    const save = vi.fn()
    const form = alertChannelFormFrom(notificationChannel({ channel_type: "custom_http", secret_present: { endpoint: true, headers: true, auth: true, hmac: true, "named_secrets.signing": true } }))
    render(<Harness initial={form} editing save={save} />)
    expect(screen.getAllByTestId("select")[0]).toBeDisabled()
    expect(screen.getByLabelText(/^Delivery address/)).toHaveValue("")
    fireEvent.click(screen.getByText("Optional settings"))
    fireEvent.click(screen.getByRole("checkbox", { name: "Clear saved Replacement request headers" }))
    fireEvent.click(screen.getByRole("checkbox", { name: "Clear saved authentication" }))
    fireEvent.click(screen.getByRole("checkbox", { name: "Clear saved named_secrets.signing" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(save.mock.calls[0][0].clearSecrets).toEqual(["headers", "auth", "named_secrets.signing"])
    expect(screen.getByLabelText("Basic authentication password")).toBeDisabled()
    fireEvent.click(screen.getByRole("checkbox", { name: "Clear saved authentication" }))
    expect(screen.getByLabelText("Basic authentication password")).not.toBeDisabled()
  })
  it("edits templates, uses the common example and changes optional values", () => {
    const save = vi.fn()
    render(<Harness initial={{ ...emptyAlertChannelForm("serverchan_turbo"), name: "Ops" }} save={save} />)
    fireEvent.click(screen.getByText("Optional settings"))
    fireEvent.click(screen.getByRole("checkbox", { name: "Hide source IP" }))
    fireEvent.click(screen.getByText("Message templates"))
    fireEvent.change(screen.getByLabelText("Subject template"), { target: { value: "{{rule.name}}" } })
    fireEvent.change(screen.getByLabelText("Message template"), { target: { value: "{{event.message}}" } })
    fireEvent.click(screen.getByRole("button", { name: "Use common template" }))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(save.mock.calls[0][0]).toMatchObject({ templateSubject: "{{rule.name}}", values: expect.objectContaining({ noip: "true" }) })
    expect(save.mock.calls[0][0].template).toContain("{{event.severity}}")
  })
  it("disables editing during a save and permits cancelling before it", () => {
    const close = vi.fn()
    const view = render(<Harness loading initial={{ ...emptyAlertChannelForm(), name: "Ops" }} close={close} />)
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    expect(screen.getByLabelText("Name")).toBeDisabled()
    view.unmount()
    render(<Harness close={close} />)
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(close).toHaveBeenCalledWith(false)
  })
})
