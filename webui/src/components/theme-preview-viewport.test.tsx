import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ThemePreviewViewport } from "@/components/theme-preview-viewport"

describe("trusted theme preview viewport", () => {
  it("creates a separate viewport with only Beat styles and React content", async () => {
    const sheet = document.createElement("link")
    sheet.rel = "stylesheet"
    sheet.href = "/assets/beat.css"
    document.head.append(sheet)
    const view = render(<ThemePreviewViewport title="Slate preview" mobile><p>Trusted content</p></ThemePreviewViewport>)
    const frame = screen.getByTitle("Slate preview") as HTMLIFrameElement
    expect(frame).toHaveAttribute("sandbox", "allow-same-origin")
    expect(frame.style.width).toBe("390px")
    const target = frame.contentDocument!
    target.body.innerHTML = '<div id="theme-preview-root"></div>'
    fireEvent.load(frame)
    await waitFor(() => expect(target.body.textContent).toBe("Trusted content"))
    expect(target.querySelectorAll('link[rel="stylesheet"]')).toHaveLength(1)
    expect(target.querySelector("script")).toBeNull()
    expect(target.title).toBe("Slate preview")
    view.rerender(<ThemePreviewViewport title="Slate preview" mobile={false}><p>Updated content</p></ThemePreviewViewport>)
    expect(target.body.textContent).toBe("Updated content")
    expect(frame.style.width).toBe("100%")
    sheet.remove()
  })
  it("tolerates a frame that has already lost its document", () => {
    render(<ThemePreviewViewport title="Unavailable preview" mobile={false}><p>content</p></ThemePreviewViewport>)
    const frame = screen.getByTitle("Unavailable preview")
    Object.defineProperty(frame, "contentDocument", { value: null })
    fireEvent.load(frame)
    expect(screen.queryByText("content")).not.toBeInTheDocument()
  })
})
