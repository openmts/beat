import { afterEach, describe, expect, it, vi } from "vitest"
import { examplePreviewURL, readExampleArchive, themeExamples } from "@/lib/theme-examples"

afterEach(() => { vi.unstubAllGlobals() })
describe("bundled theme downloads", () => {
  it("only downloads a known local ZIP", async () => {
    const archive = new Blob(["zip"])
    const fetcher = vi.fn().mockResolvedValue({ ok: true, blob: async () => archive })
    vi.stubGlobal("fetch", fetcher)
    expect(await readExampleArchive("slate")).toBe(archive)
    expect(fetcher).toHaveBeenCalledWith("/themes/slate.theme.zip")
    await expect(readExampleArchive("../../private")).rejects.toThrow("Unknown example")
    expect(fetcher).toHaveBeenCalledOnce()
  })
  it("reports missing archives instead of uploading a fallback page", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false }))
    await expect(readExampleArchive("canvas")).rejects.toThrow("unavailable")
  })

  it("points every bundled example at its own preview image", () => {
    for (const example of themeExamples) {
      expect(examplePreviewURL(example.id)).toBe(`/themes/${example.id}.preview.png`)
    }
    expect(examplePreviewURL("../../private")).toBeNull()
  })
})
