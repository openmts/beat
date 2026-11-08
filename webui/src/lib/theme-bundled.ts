export interface BundledTheme {
  id: string
  name: string
  descriptionKey: string
  archiveURL: string
  previewURL: string
}

// 随前端分发的主题只从这里登记；构建脚本负责生成同名 ZIP 与预览图。
export const bundledThemes: readonly BundledTheme[] = [
  {
    id: "default",
    name: "Default",
    descriptionKey: "theme.bundled_default",
    archiveURL: "/themes/default.theme.zip",
    previewURL: "/themes/default.preview.png",
  },
  {
    id: "paper",
    name: "Paper",
    descriptionKey: "theme.bundled_paper",
    archiveURL: "/themes/paper.theme.zip",
    previewURL: "/themes/paper.preview.png",
  },
]

export async function readBundledTheme(id: string): Promise<Blob> {
  const theme = bundledThemes.find((candidate) => candidate.id === id)
  if (!theme) throw new Error("Unknown bundled theme")
  const response = await fetch(theme.archiveURL)
  if (!response.ok) throw new Error("Bundled theme is unavailable")
  return response.blob()
}
