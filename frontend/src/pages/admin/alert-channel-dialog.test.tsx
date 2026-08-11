import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Dispatch, SetStateAction } from "react"
import { LocaleProvider } from "@/context/locale"
import AlertChannelDialog from "@/pages/admin/alert-channel-dialog"
import type { AlertChannelFormState } from "@/pages/admin/alert-channel-config"
import { emptyAlertChannelForm } from "@/pages/admin/alert-channel-config"

vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog, DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough, DialogTitle: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
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

function view(props: Partial<{
  open: boolean
  editing: boolean
  loading: boolean
  form: AlertChannelFormState
  onSave: () => void
  onOpenChange: (open: boolean) => void
}>) {
  const setForm: Dispatch<SetStateAction<AlertChannelFormState>> = vi.fn() as unknown as Dispatch<SetStateAction<AlertChannelFormState>>
  return {
    setForm,
    view: render(<LocaleProvider><AlertChannelDialog
      open={props.open ?? true}
      editing={props.editing ?? false}
      loading={props.loading ?? false}
      form={props.form ?? emptyAlertChannelForm()}
      setForm={setForm}
      onOpenChange={props.onOpenChange ?? (() => undefined)}
      onSave={props.onSave ?? (() => undefined)}
    /></LocaleProvider>),
  }
}

describe("AlertChannelDialog", () => {
  beforeEach(() => {
    localStorage.setItem("locale", "en")
  })

  it("renders webhook and telegram forms with edit hints", () => {
    const { view: rendered } = view({ editing: true })
    expect(screen.getByLabelText("Name")).toBeInTheDocument()
    expect(screen.getByLabelText("Webhook URL")).toBeInTheDocument()
    expect(screen.getByTestId("select")).toHaveAttribute("data-selected-label", "Webhook")
    rendered.unmount()
    view({ form: { ...emptyAlertChannelForm(), channelType: "telegram" }, editing: true })
    expect(screen.getByLabelText("Bot token")).toBeInTheDocument()
    expect(screen.getByText("Leave blank to keep the current credential.")).toBeInTheDocument()
    expect(screen.getByLabelText("Chat ID")).toBeInTheDocument()
  })

  it("disables save while loading or with an empty name", () => {
    const { view: rendered } = view({ loading: true })
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    rendered.unmount()
    view({ form: { ...emptyAlertChannelForm(), name: "Channel" } })
    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled()
  })

  it("edits every email field and updates security defaults", () => {
    view({ form: { ...emptyAlertChannelForm(), channelType: "email" } })
    fireEvent.change(screen.getByLabelText("SMTP host"), { target: { value: "smtp.example.com" } })
    fireEvent.change(screen.getByLabelText("Port"), { target: { value: "465" } })
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "user" } })
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "secret" } })
    fireEvent.change(screen.getByLabelText("Sender address"), { target: { value: "ops@example.com" } })
    fireEvent.change(screen.getByLabelText("Recipients"), { target: { value: "team@example.com" } })
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "Email")
    expect(selects[1]).toHaveAttribute("data-selected-label", "STARTTLS")
    fireEvent.change(selects[1], { target: { value: "tls" } })
  })

  it("edits webhook and telegram fields", () => {
    const { view: rendered } = view({ form: { ...emptyAlertChannelForm(), channelType: "webhook" } })
    fireEvent.change(screen.getByLabelText("Webhook URL"), { target: { value: "https://example.com/hook" } })
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Ops" } })
    rendered.unmount()
    view({ form: { ...emptyAlertChannelForm(), channelType: "telegram" } })
    fireEvent.change(screen.getByLabelText("Bot token"), { target: { value: "123:abc" } })
    fireEvent.change(screen.getByLabelText("Chat ID"), { target: { value: "99" } })
  })
})
