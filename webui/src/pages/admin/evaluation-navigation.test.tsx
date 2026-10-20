import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import Alerts from "@/pages/admin/alerts"
import * as api from "@/lib/alert-evaluation-api"
import { evaluationRuleFixture, evaluationSourcesFixture } from "@/test/alert-evaluation-fixtures"
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { role: "admin" } } }) }))
vi.mock("@/lib/alert-evaluation-api")
beforeEach(() => { vi.resetAllMocks(); localStorage.setItem("locale", "en"); vi.mocked(api.evaluationRules).mockResolvedValue([evaluationRuleFixture()]); vi.mocked(api.evaluationSources).mockResolvedValue(evaluationSourcesFixture()); vi.mocked(api.evaluationStates).mockResolvedValue({ items: [] }); vi.mocked(api.evaluationEvents).mockResolvedValue({ items: [] }) })
it("opens independent evaluation and health history tabs from alert administration", async () => {
  render(<MemoryRouter><LocaleProvider><Alerts /></LocaleProvider></MemoryRouter>)
  await screen.findByText("CPU high")
  fireEvent.click(screen.getByRole("tab", { name: "Evaluations" }))
  await waitFor(() => expect(api.evaluationStates).toHaveBeenCalled())
  fireEvent.click(screen.getByRole("tab", { name: "Evaluation history" }))
  await waitFor(() => expect(api.evaluationEvents).toHaveBeenCalled())
  expect(screen.queryByRole("tab", { name: "Notification settings" })).not.toBeInTheDocument()
})
