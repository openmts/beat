import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { AlertRuleDialog, type AlertRuleForm } from "@/pages/admin/alert-rule-dialog"

vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog, DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough, DialogTitle: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/admin-select-field", () => ({
  AdminSelectField: ({ id, label, options, value, disabled, onValueChange }: {
    id: string
    label: string
    options: Array<{ label: string; value: string }>
    value: string
    disabled?: boolean
    onValueChange: (value: string) => void
  }) => (
    <label>
      {label}
      <select
        id={id}
        data-testid="admin-select"
        data-selected-label={options.find((option) => option.value === value)?.label}
        disabled={disabled}
        value={value}
        onChange={(event) => onValueChange(event.target.value)}
      >
        {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
    </label>
  ),
}))

const baseForm: AlertRuleForm = {
  name: "CPU high", description: "hot", metric: "cpu", operator: "gt",
  threshold: 80, duration: 300, severity: "warning",
}

function view(props: Partial<{
  open: boolean
  editing: boolean
  form: AlertRuleForm
  loading: boolean
  onOpenChange: (open: boolean) => void
  onChange: (form: AlertRuleForm) => void
  onSave: () => void
}>) {
  const onChange = props.onChange ?? vi.fn()
  return {
    onChange,
    view: render(<LocaleProvider><AlertRuleDialog
      open={props.open ?? true}
      editing={props.editing ?? false}
      form={props.form ?? baseForm}
      loading={props.loading ?? false}
      onOpenChange={props.onOpenChange ?? (() => undefined)}
      onChange={onChange}
      onSave={props.onSave ?? (() => undefined)}
    /></LocaleProvider>),
  }
}

describe("AlertRuleDialog", () => {
  beforeEach(() => {
    localStorage.setItem("locale", "en")
  })

  it("edits name, description, threshold, and duration", () => {
    const { onChange } = view({})
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Disk high" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ name: "Disk high" }))
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "root full" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ description: "root full" }))
    fireEvent.change(screen.getByLabelText("Threshold"), { target: { value: "90" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ threshold: 90 }))
    fireEvent.change(screen.getByLabelText("Duration (seconds)"), { target: { value: "120" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ duration: 120 }))
  })

  it("switches to availability metric and applies defaults", () => {
    const { onChange } = view({})
    const selects = screen.getAllByTestId("admin-select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "CPU")
    fireEvent.change(selects[0], { target: { value: "heartbeat_age_seconds" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({
      metric: "heartbeat_age_seconds",
      operator: "gt", threshold: 90, duration: 30,
    }))
  })

  it("renders availability labels when metric is availability", () => {
    view({ form: { ...baseForm, metric: "heartbeat_age_seconds", threshold: 90, duration: 30 } })
    expect(screen.getByLabelText("Offline after (seconds)")).toBeInTheDocument()
    expect(screen.getByLabelText("Debounce (seconds)")).toBeInTheDocument()
    expect(screen.getByLabelText("Operator")).toBeDisabled()
  })

  it("renders non-availability labels and edits operator and severity", () => {
    const { onChange } = view({})
    expect(screen.getByLabelText("Threshold")).toBeInTheDocument()
    expect(screen.getByLabelText("Duration (seconds)")).toBeInTheDocument()
    const selects = screen.getAllByTestId("admin-select")
    fireEvent.change(selects[1], { target: { value: "lt" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ operator: "lt" }))
    fireEvent.change(selects[2], { target: { value: "critical" } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ severity: "critical" }))
  })

  it("disables save while loading or with an empty name", () => {
    const { view: rendered } = view({ loading: true })
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    rendered.unmount()
    view({ form: { ...baseForm, name: "" } })
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
  })
})
