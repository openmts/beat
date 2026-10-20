import { useState } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import type { OperationPolicy, OperationRoot } from "@/types/operations"
import { OperationRoots } from "./operation-roots"
import { OperationRetention } from "./operation-retention"
import { operationPolicy } from "@/test/operations-fixtures"
function Roots({ initial = [] }: { initial?: OperationRoot[] }) { const [value, change] = useState(initial); return <><OperationRoots value={value} change={change} /><output data-testid="roots">{JSON.stringify(value)}</output></> }
function Retention() { const [value, change] = useState<OperationPolicy["retention"]>(); return <><OperationRetention value={value} change={change} /><output data-testid="retention">{JSON.stringify(value)}</output></> }
it("keeps fractional bytes invalid, supports byte-size limits and enforces unique root IDs", () => {
 localStorage.setItem("locale", "en"); render(<LocaleProvider><Roots /></LocaleProvider>)
 fireEvent.click(screen.getByRole("button", { name: "Add root" }))
 fireEvent.change(screen.getByLabelText("Root ID"), { target: { value: "files" } })
 fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "Uploads" } })
 fireEvent.change(screen.getByLabelText("Maximum size (MiB)"), { target: { value: String(1.5 / 1048576) } })
 expect(screen.getByLabelText("Maximum size (MiB)")).toHaveAttribute("aria-invalid", "true")
 expect(JSON.parse(screen.getByTestId("roots").textContent!)[0].maximum_bytes).toBe(1.5)
 fireEvent.change(screen.getByLabelText("Maximum size (MiB)"), { target: { value: "" } }); expect(screen.getByLabelText("Maximum size (MiB)")).toHaveValue(null)
 fireEvent.change(screen.getByLabelText("Maximum size (MiB)"), { target: { value: String(1 / 1048576) } }); expect(screen.getByLabelText("Maximum size (MiB)")).toHaveAttribute("aria-invalid", "false")
 for (const name of ["Read", "Write", "Overwrite", "Executable"]) fireEvent.click(screen.getByRole("checkbox", { name }))
 fireEvent.click(screen.getByRole("button", { name: "Add root" })); fireEvent.change(screen.getAllByLabelText("Root ID")[1], { target: { value: "files" } })
 expect(screen.getAllByLabelText("Root ID")[0]).toHaveAttribute("aria-invalid", "true")
 fireEvent.click(screen.getAllByRole("button", { name: "Remove" })[1]); fireEvent.click(screen.getByRole("button", { name: "Remove" })); expect(screen.queryByLabelText("Root ID")).not.toBeInTheDocument()
})
it("keeps explicit zero defaults visible and does not silently clamp output retention", () => {
 localStorage.setItem("locale", "en"); render(<LocaleProvider><Retention /></LocaleProvider>)
 expect(screen.getByLabelText("Task records (days)")).toHaveValue(30)
 fireEvent.change(screen.getByLabelText("Output (days)"), { target: { value: "10" } }); fireEvent.change(screen.getByLabelText("Task records (days)"), { target: { value: "2" } })
 expect(screen.getByLabelText("Output (days)")).toHaveValue(10); expect(screen.getByLabelText("Output (days)")).toHaveAttribute("aria-invalid", "true")
 fireEvent.change(screen.getByLabelText("Output (days)"), { target: { value: "0" } }); expect(screen.getByLabelText("Output (days)")).toHaveValue(0); expect(screen.getByLabelText("Output (days)")).toHaveAttribute("aria-invalid", "false")
 fireEvent.change(screen.getByLabelText("Task records (days)"), { target: { value: "" } }); expect(screen.getByLabelText("Task records (days)")).toHaveValue(null)
 fireEvent.change(screen.getByLabelText("Files (hours)"), { target: { value: "25" } }); expect(screen.getByLabelText("Files (hours)")).toHaveAttribute("aria-invalid", "true")
 fireEvent.change(screen.getByLabelText("Queue (hours)"), { target: { value: "24" } }); fireEvent.change(screen.getByLabelText("Upload (hours)"), { target: { value: "6" } })
})
it("bounds roots to 64 and provides Chinese field labels", () => {
 localStorage.setItem("locale", "zh-CN"); render(<LocaleProvider><Roots initial={Array.from({ length: 64 }, (_, index) => ({ ...operationPolicy().policy.roots[0], id: `root-${index}` }))} /><Retention /></LocaleProvider>)
 expect(screen.getByText("添加根目录").closest("button")).toBeDisabled(); expect(screen.getByLabelText("任务记录（天）")).toHaveValue(30)
}, 15000)
