import { describe, expect, it } from "vitest"

import { diagnosticsTranslations } from "@/context/diagnostics-translations"
import { zhCNDiagnostics } from "@/context/zh-cn/diagnostics"

describe("diagnostics translations", () => {
  it("keeps the English and Simplified Chinese catalogs in lockstep", () => {
    const english = Object.keys(diagnosticsTranslations.en).sort()
    const chinese = Object.keys(zhCNDiagnostics).sort()
    expect(chinese).toEqual(english)
    expect(english.length).toBeGreaterThan(90)
  })

  it("uses Simplified Chinese only in the zh-CN catalog", () => {
    const chinesePattern = /[\u4e00-\u9fff]/
    for (const [key, value] of Object.entries(zhCNDiagnostics)) {
      expect(chinesePattern.test(value), key).toBe(true)
    }
    for (const [key, value] of Object.entries(diagnosticsTranslations.en)) {
      expect(chinesePattern.test(value), key).toBe(false)
    }
  })

  it("covers every job state, kind and timeline step the console renders", () => {
    const states = ["queued", "leased", "running", "succeeded", "partial", "failed", "timed_out", "cancelled", "interrupted", "unknown"]
    for (const state of states) expect(diagnosticsTranslations.en[`diagnostics.state.${state}`]).toBeTruthy()
    for (const kind of ["mtr", "iperf3"]) expect(diagnosticsTranslations.en[`diagnostics.kind.${kind}`]).toBeTruthy()
    for (const step of ["authorized", "leased", "started", "measured", "completed"]) {
      expect(diagnosticsTranslations.en[`diagnostics.timeline.${step}`]).toBeTruthy()
    }
  })

  it("keeps the job-action keys reachable from the confirm dialog", () => {
    for (const action of ["cancel", "retry", "delete"]) {
      expect(diagnosticsTranslations.en[`diagnostics.${action}_confirm_title`]).toBeTruthy()
      expect(diagnosticsTranslations.en[`diagnostics.${action}_confirm_body`]).toBeTruthy()
    }
  })
})
