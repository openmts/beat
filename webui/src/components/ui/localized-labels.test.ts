import { describe, expect, it } from "vitest"

// 无障碍属性是界面文案的一部分：屏幕阅读器会逐字念出来，所以必须走文案目录。
// 唯一允许保留英文字面量的位置是「不是文案」的技术标识，逐条登记原因。
const allowedLiterals = new Map<string, string>([
  ["pages/admin/agent-setup-panels.tsx:agent.json", "配置文件块标题就是文件名本身"],
  ["pages/admin/operation-task-detail.tsx:stdout", "命令输出块标题对应真实流名称"],
  ["pages/admin/operation-task-detail.tsx:stderr", "命令输出块标题对应真实流名称"],
])

const patterns = [
  { name: "aria-label", pattern: /aria-label="([^"]+)"/g },
  { name: "title", pattern: /title="([^"]+)"/g },
  { name: "alt", pattern: /alt="([^"]+)"/g },
  { name: "sr-only", pattern: /className="sr-only">([^<]*)</g },
  // 组件签名里的默认值：`label = "Toggle sidebar",` 这种写法同样会把英文带到界面上。
  // 行尾必须正好是逗号，避免把多行 JSX 里的 placeholder 属性也算进来。
  { name: "default", pattern: /^[ \t]*(?:label|title|placeholder|alt)\s*=\s*"([^"]+)",[ \t]*$/gm },
]

// 只扫描以 ASCII 字母开头的字面量：中文、符号、插值表达式都不是硬编码英文。
const englishLiteral = /^[A-Za-z]/

// 用 Vite 自带的 raw glob 读源码：测试环境里同样可用，且不必引入 node 类型定义。
const bundledSources = import.meta.glob("../../**/*.tsx", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>

function sourceEntries(): { file: string; text: string }[] {
  return Object.entries(bundledSources)
    .filter(([path]) => !path.endsWith(".test.tsx"))
    .map(([path, text]) => ({ file: path.replace(/^\.\.\/\.\.\//, "").replace(/^\.\//, ""), text }))
}

function hardcodedLabels(): string[] {
  const found: string[] = []
  for (const { file, text } of sourceEntries()) {
    for (const { pattern } of patterns) {
      for (const match of text.matchAll(pattern)) {
        const value = match[1].trim()
        if (!englishLiteral.test(value)) continue
        if (!allowedLiterals.has(`${file}:${value}`)) found.push(`${file}:${value}`)
      }
    }
  }
  return found
}

describe("hardcoded accessible labels", () => {
  it("keeps every accessible label in the translation catalog", () => {
    expect(hardcodedLabels()).toEqual([])
  })

  it("only allows technical literals that still exist", () => {
    const entries = sourceEntries()
    const stale = [...allowedLiterals.keys()].filter((key) => {
      const index = key.lastIndexOf(":")
      const file = key.slice(0, index)
      const value = key.slice(index + 1)
      return !entries.some((entry) => entry.file === file && entry.text.includes(`"${value}"`))
    })
    expect(stale).toEqual([])
  })
})
