import { afterEach, describe, expect, it, vi } from "vitest"
import { bundledThemes, readBundledTheme } from "@/lib/theme-bundled"

describe("bundled theme downloads", () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it("reads the registered archive from the packaged asset path", async () => {
    const archive = new Blob(["zip"])
    const fetchMock = vi.fn().mockResolvedValue(new Response(archive, { status: 200 }))
    vi.stubGlobal("fetch", fetchMock)
    expect(await readBundledTheme("default")).toBeInstanceOf(Blob)
    expect(fetchMock).toHaveBeenCalledWith("/themes/default.theme.zip")
    expect(bundledThemes[0]).toMatchObject({ id: "default", previewURL: "/themes/default.preview.png" })
    expect(bundledThemes[1]).toMatchObject({ id: "paper", archiveURL: "/themes/paper.theme.zip", previewURL: "/themes/paper.preview.png" })
  })

  it("rejects unknown ids and unavailable assets", async () => {
    await expect(readBundledTheme("missing")).rejects.toThrow("Unknown bundled theme")
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 404 })))
    await expect(readBundledTheme("default")).rejects.toThrow("Bundled theme is unavailable")
  })
})
