import { beforeEach, describe, expect, it } from "vitest"
import {
  adminLocalePending,
  hasAdminLocale,
  loadAdminLocale,
  loadedAdminCatalogs,
  resetAdminLocaleCatalogs,
} from "@/context/admin-locales"

describe("admin locale shards", () => {
  beforeEach(() => {
    resetAdminLocaleCatalogs()
  })

  it("never pends on the inline English catalog", () => {
    expect(adminLocalePending("en")).toBe(false)
    expect(hasAdminLocale("en")).toBe(false)
  })

  it("loads a locale shard once and then reports it as ready", async () => {
    expect(adminLocalePending("zh-TW")).toBe(true)
    expect(await loadAdminLocale("zh-TW")).toBe(true)
    expect(adminLocalePending("zh-TW")).toBe(false)
    expect(await loadAdminLocale("zh-TW")).toBe(false)
    expect(loadedAdminCatalogs()).toHaveLength(1)
    expect(loadedAdminCatalogs()[0]?.["zh-TW"]?.["security.title"]).toBeTruthy()
  })

  it("keeps every shipped non-English locale loadable", async () => {
    for (const locale of ["zh-CN", "ja", "id"] as const) {
      expect(await loadAdminLocale(locale)).toBe(true)
    }
    expect(loadedAdminCatalogs()).toHaveLength(3)
  })

  it("drops loaded shards when reset for test isolation", async () => {
    await loadAdminLocale("ja")
    expect(hasAdminLocale("ja")).toBe(true)
    resetAdminLocaleCatalogs()
    expect(hasAdminLocale("ja")).toBe(false)
    expect(adminLocalePending("ja")).toBe(true)
  })
})
