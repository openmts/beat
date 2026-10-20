import { validateLifecycleDraft, parseDayOffsets } from "@/lib/lifecycle-validation"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NodeLifecycleEditor } from "./node-lifecycle-editor"
import { NodeLocationEditor } from "./node-location-editor"
import { GeoIPSettingsEditor } from "./geoip-settings-editor"
import { LifecycleSettingsEditor } from "./lifecycle-settings-editor"
import { MMDBManager } from "./mmdb-manager"
import * as api from "@/lib/lifecycle-api"
import type { GeoIPSettings, LifecycleSettings, NodeLifecycle, NodeLocation } from "@/types/node-lifecycle"
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" } } }) }))
vi.mock("@/lib/lifecycle-api", () => ({ saveLifecycle: vi.fn(), saveLocation: vi.fn(), refreshLocation: vi.fn(), saveGeoIPSettings: vi.fn(), saveLifecycleSettings: vi.fn(), uploadMMDB: vi.fn(), updateMMDB: vi.fn(), rollbackMMDB: vi.fn() }))
const lifecycle = (): NodeLifecycle => ({ node_id: "node", provider: "Provider", plan: "Plan", price_micros: "9223372036854775807", currency: "USD", interval_unit: "month", interval_count: 1, expires_on: "2027-01-01", renewal_mode: "manual", private_notes: "private", expiry_visibility: "private", price_visibility: "private", confirm_auto_advance: false, status: "active", days_remaining: 90, anchor_month: 1, anchor_day: 1, anchor_end_of_month: false, updated_at: "" })
const location = (): NodeLocation => ({ node_id: "node", scope: "public", country_code: "JP", mode: "manual", visibility: "private", provider: "mmdb", revision: "1", stale: false, has_source: false })
const geo = (): GeoIPSettings => ({ provider: "disabled", revision: 7, remote_consent: false, has_credential: true, mmdb: { edition: "GeoLite2-Country", source: "uploaded", build_epoch: 1, sha256: "hash", size_bytes: 100, state: "ready" } })
const settings: LifecycleSettings = { timezone: "UTC", lead_days: [0, 7], post_days: [1], public_country: false, public_expiry: false, public_price: false }
beforeEach(() => vi.clearAllMocks())
async function confirm() { await userEvent.click(await screen.findByRole("button", { name: "Confirm" })) }
describe("node lifecycle and location editors", () => {
 it("saves exact micros and requires explicit automatic-date consent", async () => {
  const saved = vi.fn(); render(<NodeLifecycleEditor value={lifecycle()} writable saved={saved} />)
  expect(screen.getByLabelText("Price (up to 6 decimals)")).toHaveValue("9223372036854.775807")
  fireEvent.change(screen.getByLabelText("Date advancement"), { target: { value: "calendar_auto_advance" } })
  expect(screen.getByRole("button", { name: "Save lifecycle" })).toBeDisabled()
  await userEvent.click(screen.getByRole("checkbox", { name: /date advancement/ }))
  await userEvent.click(screen.getByRole("button", { name: "Save lifecycle" }))
  await waitFor(() => expect(saved).toHaveBeenCalledOnce())
  expect(api.saveLifecycle).toHaveBeenCalledWith("node", expect.objectContaining({ price_micros: "9223372036854775807", confirm_auto_advance: true }))
 })
 it("edits all lifecycle fields, handles errors, and validates boundaries", async () => {
  vi.mocked(api.saveLifecycle).mockRejectedValueOnce(new Error("conflict")); render(<NodeLifecycleEditor value={lifecycle()} writable saved={vi.fn()} />)
  for (const [label, value] of [["Provider", "New"], ["Plan", "Small"], ["Price (up to 6 decimals)", "1.000001"], ["Currency (ISO 4217)", "eur"], ["Interval count (1–120)", "2"], ["Expiry date", "2028-02-29"], ["Private notes", "memo"]]) fireEvent.change(screen.getByLabelText(label), { target: { value } })
  for (const [label, value] of [["Interval unit", "year"], ["Expiry visibility", "public"], ["Price visibility", "inherit"]]) fireEvent.change(screen.getByLabelText(label), { target: { value } })
  await userEvent.click(screen.getByRole("button", { name: "Save lifecycle" })); expect(await screen.findByRole("alert")).toHaveTextContent("conflict")
  expect(api.saveLifecycle).toHaveBeenCalledWith("node", expect.objectContaining({ currency: "EUR", price_micros: "1000001", interval_unit: "year" }))
  for (const draft of [{ provider: "界".repeat(34) }, { interval_count: 121 }, { expires_on: "2027-02-29" }, { currency: "bad" }, { renewal_mode: "calendar_auto_advance" as const }]) expect(() => validateLifecycleDraft({ ...lifecycle(), ...draft }, "1")).toThrow()
  expect(() => validateLifecycleDraft({ ...lifecycle(), renewal_mode: "calendar_auto_advance" }, "1", "calendar_auto_advance")).not.toThrow()
 })
 it("requires consent for a new managed target but preserves an existing target for visibility edits", async () => {
  const saved = vi.fn(); render(<NodeLocationEditor value={{ ...location(), mode: "managed_target" }} writable saved={saved} />)
  fireEvent.change(screen.getByLabelText("Visibility"), { target: { value: "public" } }); await userEvent.click(screen.getByRole("button", { name: "Save location" })); await waitFor(() => expect(saved).toHaveBeenCalledOnce())
  expect(api.saveLocation).toHaveBeenCalledWith("node", expect.objectContaining({ managed_target: "", confirm_managed_target: false, visibility: "public" }))
  fireEvent.change(screen.getByLabelText("Target IP or hostname"), { target: { value: "example.com" } }); expect(screen.getByRole("button", { name: "Save location" })).toBeDisabled()
  await userEvent.click(screen.getByRole("checkbox", { name: /authorize/ })); await userEvent.click(screen.getByRole("button", { name: "Save location" })); expect(api.saveLocation).toHaveBeenLastCalledWith("node", expect.objectContaining({ managed_target: "example.com", confirm_managed_target: true }))
 })
 it("supports manual scope/country, purge, refresh, disabled state and errors", async () => {
  vi.mocked(api.refreshLocation).mockRejectedValueOnce(new Error("unavailable")); render(<NodeLocationEditor value={{ ...location(), stale: true, error_code: "provider_failed" }} writable saved={vi.fn()} />)
  fireEvent.change(screen.getByLabelText("Country code (ISO 3166-1)"), { target: { value: "us" } }); await userEvent.click(screen.getByRole("checkbox", { name: /Purge/ }))
  fireEvent.change(screen.getByLabelText("Address scope"), { target: { value: "private" } }); expect(screen.queryByLabelText("Country code (ISO 3166-1)")).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText("Location source"), { target: { value: "disabled" } }); await userEvent.click(screen.getByRole("button", { name: "Save location" }))
  expect(api.saveLocation).toHaveBeenCalledWith("node", expect.objectContaining({ mode: "disabled", country_code: "", purge: true }))
  await userEvent.click(screen.getByRole("button", { name: "Refresh location" })); expect(await screen.findByRole("alert")).toHaveTextContent("unavailable")
 })
})
describe("global lifecycle and GeoIP editors", () => {
 it("validates date stages and saves visibility through explicit confirmation", async () => {
  const saved = vi.fn(); render(<LifecycleSettingsEditor value={settings} saved={saved} />)
  fireEvent.change(screen.getByLabelText("Timezone"), { target: { value: "America/New_York" } }); fireEvent.change(screen.getByLabelText(/before expiry/), { target: { value: "7,0,7,3" } }); fireEvent.change(screen.getByLabelText(/after expiry/), { target: { value: "1,7" } })
  for (const label of ["Public country by default", "Public expiry by default", "Public price by default"]) await userEvent.click(screen.getByRole("checkbox", { name: label }))
  await userEvent.click(screen.getByRole("button", { name: "Save defaults" })); await confirm(); await waitFor(() => expect(saved).toHaveBeenCalledOnce())
  expect(api.saveLifecycleSettings).toHaveBeenCalledWith(expect.objectContaining({ lead_days: [0, 3, 7], post_days: [1, 7], public_price: true }))
  expect(parseDayOffsets("")).toEqual([]); for (const value of ["-1", "366", "x", Array(367).fill("1").join(",")]) expect(() => parseDayOffsets(value)).toThrow(); expect(() => parseDayOffsets("0", true)).toThrow()
  fireEvent.change(screen.getByLabelText("Timezone"), { target: { value: "bad-zone" } }); await userEvent.click(screen.getByRole("button", { name: "Save defaults" })); expect(await screen.findByRole("alert")).toHaveTextContent(/time zone|timezone/i)
 })
 it("requires named remote consent, sends CAS and clears transient credential", async () => {
  render(<GeoIPSettingsEditor value={geo()} saved={vi.fn()} />)
  fireEvent.change(screen.getByLabelText("Provider"), { target: { value: "ipinfo_https" } }); fireEvent.change(screen.getByLabelText(/New credential/), { target: { value: "secret" } })
  expect(screen.getByRole("button", { name: "Save GeoIP settings" })).toBeDisabled()
  await userEvent.click(screen.getByRole("checkbox", { name: /consent to sending.*IPinfo over HTTPS/ })); await userEvent.click(screen.getByRole("button", { name: "Save GeoIP settings" })); await confirm()
  await waitFor(() => expect(api.saveGeoIPSettings).toHaveBeenCalledWith(expect.objectContaining({ provider: "ipinfo_https", credential: "secret", expected_revision: 7, remote_consent: true })))
  expect(screen.getByLabelText(/New credential/)).toHaveValue("")
  await userEvent.click(screen.getByRole("checkbox", { name: "Clear existing credential" })); await userEvent.click(screen.getByRole("checkbox", { name: /Also purge/ })); await userEvent.click(screen.getByRole("button", { name: "Save GeoIP settings" })); await confirm()
  await waitFor(() => expect(api.saveGeoIPSettings).toHaveBeenLastCalledWith(expect.objectContaining({ credential: "", purge: true })))
 })
 it("uploads, downloads and rolls back MMDB with the loaded revision", async () => {
  const saved = vi.fn(); render(<MMDBManager value={geo()} saved={saved} />)
  const file = new File(["database"], "country.mmdb", { type: "application/octet-stream" }); fireEvent.change(screen.getByLabelText(/Upload country database/), { target: { files: [file] } })
  await userEvent.click(screen.getByRole("button", { name: "Upload" })); await confirm(); await waitFor(() => expect(api.uploadMMDB).toHaveBeenCalledWith(file, 7))
  fireEvent.change(screen.getByLabelText("MaxMind account ID"), { target: { value: "account" } }); fireEvent.change(screen.getByLabelText("MaxMind transient license key"), { target: { value: "license" } }); fireEvent.change(screen.getByLabelText("Edition"), { target: { value: "GeoIP2-Country" } })
  await userEvent.click(screen.getByRole("button", { name: "Download from MaxMind" })); await confirm(); await waitFor(() => expect(api.updateMMDB).toHaveBeenCalledWith({ account_id: "account", license_key: "license", edition: "GeoIP2-Country", expected_revision: 7 }))
  expect(screen.getByLabelText("MaxMind transient license key")).toHaveValue(""); await userEvent.click(screen.getByRole("button", { name: "Roll back database" })); await confirm(); await waitFor(() => expect(api.rollbackMMDB).toHaveBeenCalledWith(7))
  expect(within(screen.getByRole("button", { name: "Upload" })).queryByRole("alert")).toBeNull()
 })
})
