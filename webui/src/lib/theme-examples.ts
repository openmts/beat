export const themeExamples = [{ id: "slate", name: "Slate" }, { id: "operations", name: "Operations" }, { id: "canvas", name: "Canvas" }, { id: "graphite", name: "Graphite" }, { id: "aurora", name: "Aurora" }, { id: "terminal", name: "Terminal" }, { id: "paper", name: "Paper" }, { id: "midnight", name: "Midnight" }, { id: "solar", name: "Solar" }, { id: "pulse", name: "Pulse" }]

/** examplePreviewURL 返回与示例 ZIP 同源的预览图地址；未知 id 直接返回 null，避免拼出越界路径。 */
export function examplePreviewURL(id: string): string | null {
  return themeExamples.some((example) => example.id === id) ? `/themes/${id}.preview.png` : null
}

export async function readExampleArchive(id: string): Promise<Blob> {
  if (!themeExamples.some((example) => example.id === id)) throw new Error("Unknown example theme")
  const response = await fetch(`/themes/${id}.theme.zip`)
  if (!response.ok) throw new Error("Theme example is unavailable")
  return response.blob()
}
