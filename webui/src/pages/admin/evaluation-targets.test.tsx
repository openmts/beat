import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { EvaluationTargets } from "@/pages/admin/evaluation-targets"
it("bounds visible choices and preserves missing references for explicit removal", () => {
  localStorage.setItem("locale", "en")
  const change = vi.fn(), items = Array.from({ length: 101 }, (_, index) => ({ id: `id-${index}`, name: `Node ${index}` }))
  render(<LocaleProvider><EvaluationTargets items={items} value={["deleted"]} limit={1} onChange={change} /></LocaleProvider>)
  expect(screen.getByText(/Showing the first 100/)).toBeInTheDocument(); expect(screen.getAllByRole("checkbox")).toHaveLength(100)
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "deleted" } })
  fireEvent.click(screen.getByRole("checkbox", { name: /deleted/ })); expect(change).toHaveBeenCalledWith([])
})
it("translates local fields and supports selection and deselection", () => {
  localStorage.setItem("locale", "zh-CN")
  const change = vi.fn()
  render(<LocaleProvider><EvaluationTargets items={[{ id: "one", name: "节点一" }]} value={[]} limit={8} onChange={change} /></LocaleProvider>)
  expect(screen.getByPlaceholderText("搜索目标")).toBeInTheDocument(); fireEvent.click(screen.getByRole("checkbox", { name: "节点一" })); expect(change).toHaveBeenCalledWith(["one"])
})
