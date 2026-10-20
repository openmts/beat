import MockAdapter from "axios-mock-adapter"
import { afterEach, expect, it, vi } from "vitest"
import { api } from "@/lib/api-client"
import {
  cancelRuntimeCapture, captureFileName, downloadRuntimeCapture, getRuntimeDiagnosticsSummary,
  listRuntimeCaptures, saveCapturedProfile, startRuntimeCapture,
} from "@/lib/runtime-diagnostics-api"
import type { RuntimeCapture } from "@/types/runtime-diagnostics"

const mock = new MockAdapter(api)
afterEach(() => { mock.reset(); vi.unstubAllGlobals() })

function capture(patch: Partial<RuntimeCapture> = {}): RuntimeCapture {
  return { id: "a1b2c3", kind: "cpu", status: "complete", started_at: "2026-10-18T00:00:00Z",
    deadline: "2026-10-18T00:00:30Z", expires_at: "2026-10-25T00:00:00Z", bytes: 2048, ...patch }
}

it("reads the summary and capture list from the admin endpoints", async () => {
  const signal = new AbortController().signal
  mock.onGet("/admin/runtime/summary").reply(200, { code: 200, data: { version: "1.0.0", uptime_seconds: 12 } })
  mock.onGet("/admin/runtime/captures").reply(200, { code: 200, data: { captures: [capture()], busy: true } })
  expect((await getRuntimeDiagnosticsSummary(signal)).version).toBe("1.0.0")
  const page = await listRuntimeCaptures(signal)
  expect(page.busy).toBe(true)
  expect(page.captures[0].id).toBe("a1b2c3")
})

it("starts and cancels a capture, encoding the identifier in the path", async () => {
  mock.onPost("/admin/runtime/captures").reply(201, { code: 201, data: capture({ status: "running" }) })
  mock.onDelete("/admin/runtime/captures/a1b2c3").reply(204)
  expect((await startRuntimeCapture({ kind: "cpu", duration_seconds: 10 })).status).toBe("running")
  expect(JSON.parse(mock.history.post[0].data)).toEqual({ kind: "cpu", duration_seconds: 10 })
  await expect(cancelRuntimeCapture("a1b2c3")).resolves.toBeUndefined()
  expect(mock.history.delete[0].url).toBe("/admin/runtime/captures/a1b2c3")
})

it("downloads the profile as a blob", async () => {
  mock.onGet("/admin/runtime/captures/a1b2c3/download").reply(200, new Blob(["pprof"], { type: "application/octet-stream" }))
  const blob = await downloadRuntimeCapture("a1b2c3")
  expect(blob.size).toBe(5)
  expect(mock.history.get[0].responseType).toBe("blob")
})

it("builds a safe download name and falls back for ids it does not trust", () => {
  expect(captureFileName(capture())).toBe("cpu-a1b2c3.prof")
  expect(captureFileName(capture({ id: "../../etc/passwd" }))).toBe("capture.prof")
  expect(captureFileName(capture({ id: "A1B2C3" }))).toBe("capture.prof")
  expect(captureFileName(capture({ kind: "cpu1" }))).toBe("capture.prof")
})

it("saves a captured profile through an anchor and revokes the object URL", () => {
  const createObjectURL = vi.fn(() => "blob:profile")
  const revokeObjectURL = vi.fn()
  vi.stubGlobal("URL", { createObjectURL, revokeObjectURL })
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined)
  saveCapturedProfile(capture(), new Blob(["pprof"]))
  expect(createObjectURL).toHaveBeenCalledTimes(1)
  expect(click).toHaveBeenCalledTimes(1)
  expect(document.querySelector("a")).toBeNull()
  click.mockRestore(); vi.useFakeTimers(); vi.runAllTimers(); vi.useRealTimers()
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:profile")
})
