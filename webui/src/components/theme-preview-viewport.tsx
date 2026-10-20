import { useState, type ReactNode, type SyntheticEvent } from "react"
import { createPortal } from "react-dom"

const previewDocument = '<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body style="margin:0"><div id="theme-preview-root"></div></body></html>'

// 仅渲染 Beat 自身组件；包中的 HTML/JS 使用另一套不透明 sandbox。
export function ThemePreviewViewport({ title, mobile, children }: { title: string; mobile: boolean; children: ReactNode }) {
  const [mount, setMount] = useState<HTMLElement | null>(null)
  const loaded = (event: SyntheticEvent<HTMLIFrameElement>) => {
    const target = event.currentTarget.contentDocument
    if (!target) return
    target.title = title
    for (const existing of target.querySelectorAll("[data-beat-preview-style]")) existing.remove()
    for (const stylesheet of document.querySelectorAll('link[rel="stylesheet"], style')) {
      const copy = stylesheet.cloneNode(true) as HTMLElement
      copy.setAttribute("data-beat-preview-style", "")
      target.head.appendChild(copy)
    }
    setMount(target.getElementById("theme-preview-root"))
  }
  return <div className="overflow-auto rounded-lg border bg-muted p-2 sm:p-4">
    <iframe title={title} srcDoc={previewDocument} sandbox="allow-same-origin" onLoad={loaded}
      className="mx-auto block h-[60vh] max-w-full border-0 bg-background" style={{ width: mobile ? 390 : "100%" }} />
    {mount && createPortal(children, mount)}
  </div>
}
