import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import Alerts from "@/pages/admin/alerts"
import * as api from "@/lib/alert-evaluation-api"
import { evaluationRuleFixture, evaluationSourcesFixture } from "@/test/alert-evaluation-fixtures"

vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { role: "admin" } } }) }))
vi.mock("@/lib/alert-evaluation-api")

beforeEach(() => {
  vi.resetAllMocks()
  localStorage.setItem("locale", "en")
  vi.mocked(api.evaluationRules).mockResolvedValue([evaluationRuleFixture()])
  vi.mocked(api.evaluationSources).mockResolvedValue(evaluationSourcesFixture())
  vi.mocked(api.evaluationStates).mockResolvedValue({ items: [] })
  vi.mocked(api.evaluationEvents).mockResolvedValue({ items: [] })
})

it("rule row history link opens the filtered evaluation history tab", async () => {
  render(<MemoryRouter initialEntries={["/admin/alerts"]}><LocaleProvider><Alerts /></LocaleProvider></MemoryRouter>)
  await screen.findByText("CPU high")
  fireEvent.click(screen.getByRole("link", { name: "Evaluation history CPU high" }))
  await waitFor(() => expect(vi.mocked(api.evaluationEvents)).toHaveBeenCalledWith(expect.stringContaining("rule_id=rule-one"), expect.anything()))
  expect(screen.getByRole("tab", { name: "Evaluation history" })).toHaveAttribute("aria-selected", "true")
})
