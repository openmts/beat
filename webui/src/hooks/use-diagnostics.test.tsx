import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useDiagnosticJobDetail, useDiagnosticJobs, useDiagnosticPolicy, useDiagnosticSerieses, useDiagnosticTargets } from "@/hooks/use-diagnostics"
import * as diagnosticsApi from "@/lib/diagnostics-api"
import type { DiagnosticState } from "@/lib/diagnostics-api"

vi.mock("@/lib/diagnostics-api")

const listJobs = vi.mocked(diagnosticsApi.listDiagnosticJobs)
const getPolicy = vi.mocked(diagnosticsApi.getDiagnosticPolicy)
const getJob = vi.mocked(diagnosticsApi.getDiagnosticJob)
const getSeries = vi.mocked(diagnosticsApi.getDiagnosticSeries)

afterEach(() => {
  vi.useRealTimers()
  vi.resetAllMocks()
})

beforeEach(() => {
  getPolicy.mockResolvedValue({ enabled: true } as never)
  listJobs.mockResolvedValue([] as never)
  getJob.mockResolvedValue({ job: { id: "job-1" }, hops: [] } as never)
  getSeries.mockResolvedValue({ metric: "m", unit: "", points: [] } as never)
  vi.mocked(diagnosticsApi.listDiagnosticTargets).mockResolvedValue([] as never)
})

describe("diagnostic resource hooks", () => {
  it("loads a resource once and exposes it", async () => {
    const { result } = renderHook(() => useDiagnosticPolicy())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toEqual({ enabled: true })
    expect(result.current.error).toBeNull()
    expect(getPolicy).toHaveBeenCalledTimes(1)
  })

  it("keeps the last good value when a silent refresh fails", async () => {
    const { result } = renderHook(() => useDiagnosticTargets())
    await waitFor(() => expect(result.current.loading).toBe(false))
    vi.mocked(diagnosticsApi.listDiagnosticTargets).mockRejectedValueOnce(new Error("offline"))
    await act(async () => { await result.current.refresh() })
    expect(result.current.error).toBe("offline")
    expect(result.current.data).toEqual([])
  })

  it("reports a non-error rejection without losing the message contract", async () => {
    getPolicy.mockReset()
    getPolicy.mockRejectedValueOnce("nope")
    const { result } = renderHook(() => useDiagnosticPolicy())
    await waitFor(() => expect(result.current.error).toBe("Unknown error"))
  })

  it("reloads jobs whenever the filter changes", async () => {
    const { rerender } = renderHook(({ state }: { state: DiagnosticState | "" }) => useDiagnosticJobs({ state }), { initialProps: { state: "" } })
    await waitFor(() => expect(listJobs).toHaveBeenCalledTimes(1))
    rerender({ state: "failed" })
    await waitFor(() => expect(listJobs).toHaveBeenLastCalledWith({ state: "failed" }))
  })

  it("polls capabilities on the requested interval", async () => {
    vi.useFakeTimers()
    vi.mocked(diagnosticsApi.listDiagnosticCapabilities).mockResolvedValue([] as never)
    const { result, unmount } = renderHook(() => useDiagnosticJobs({}, 1000))
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(listJobs).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(listJobs).toHaveBeenCalledTimes(2)
    unmount()
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(listJobs).toHaveBeenCalledTimes(2)
    expect(result.current.loading).toBe(false)
  })

  it("does not read a job while the detail sheet is closed", async () => {
    const { result } = renderHook(() => useDiagnosticJobDetail(null))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(getJob).not.toHaveBeenCalled()
  })

  it("reads a job and its series when one is selected", async () => {
    const { result } = renderHook(() => useDiagnosticJobDetail("job-1"))
    await waitFor(() => expect(result.current.data?.job).toEqual({ id: "job-1" }))
    expect(getJob).toHaveBeenCalledWith("job-1")

    const series = renderHook(() => useDiagnosticSerieses("job-1", [{ metric: "a" }, { metric: "b" }]))
    await waitFor(() => expect(series.result.current.loading).toBe(false))
    expect(getSeries).toHaveBeenCalledTimes(2)
    expect(series.result.current.series).toHaveLength(2)
  })

  it("clears series state when no job is selected and surfaces read errors", async () => {
    const empty = renderHook(() => useDiagnosticSerieses(null, [{ metric: "a" }]))
    expect(empty.result.current.series).toEqual([])
    expect(getSeries).not.toHaveBeenCalled()

    getSeries.mockRejectedValueOnce(new Error("results unavailable"))
    const failing = renderHook(() => useDiagnosticSerieses("job-1", [{ metric: "a" }]))
    await waitFor(() => expect(failing.result.current.error).toBe("results unavailable"))
    expect(failing.result.current.loading).toBe(false)
  })
})
