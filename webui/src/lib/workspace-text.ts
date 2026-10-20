// 文本面板：对齐 Grafana 的 Text panel，让看板能写说明、阈值口径与排障入口。
// 内容用最小 Markdown 子集渲染（标题、列表、代码块、行内代码、粗体、斜体、链接），
// 解析结果是纯数据结构，渲染时由 React 负责转义，因此不存在 HTML 注入面。

import type { DashboardPanel } from "@/types/metric-workspace"

/** 文本上限与 model.MaxDashboardTextBytes 对齐：4 KiB 够写说明，又不至于顶爆看板总配额。 */
export const maximumTextBytes = 4096

export interface WorkspaceTextSpan { kind: "text" | "code" | "strong" | "em" | "break"; value: string }
export interface WorkspaceTextLink { kind: "link"; value: string; url: string }
export type WorkspaceTextInline = WorkspaceTextSpan | WorkspaceTextLink

/** 列对齐来自分隔行里的冒号（`:---` 左、`:---:` 居中、`---:` 右）；null 表示不加对齐样式。 */
export type WorkspaceTextAlign = "left" | "center" | "right" | null

/** 表格单元格：解析后的行内片段，加上合并跨度与被合并前所在的列号（用于沿用该列的对齐）。
 *  column 是锚定格在原始行里的下标，跨列时不等于渲染顺序。被合并掉的格子不会出现在行里。 */
export interface WorkspaceTextTableCell { column: number; spans: WorkspaceTextInline[]; rowSpan: number; colSpan: number }

export type WorkspaceTextBlock =
  | { kind: "heading"; level: number; spans: WorkspaceTextInline[] }
  | { kind: "paragraph"; spans: WorkspaceTextInline[] }
  | { kind: "list"; ordered: boolean; items: WorkspaceTextInline[][] }
  | { kind: "table"; headers: WorkspaceTextInline[][]; aligns: WorkspaceTextAlign[]; rows: WorkspaceTextTableCell[][] }
  | { kind: "code"; value: string }

/** 文本内容只接受 UTF-8 文本：除换行、回车、制表符外不允许控制字符（粘贴的 CRLF 要能保存）。 */
export function validWorkspaceText(value: string): boolean {
  if (new TextEncoder().encode(value).length > maximumTextBytes) return false
  return !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value)
}

/** 文本面板可以空着先建占位；反过来，非文本面板带内容一律拒绝，避免保存出永远不渲染的配置。 */
export function textPanelError(panel: DashboardPanel): string | null {
  if (panel.type !== "text") return panel.text ? "workspace.text_invalid" : null
  return validWorkspaceText(panel.text ?? "") ? null : "workspace.text_invalid"
}

/** 只接受绝对 http(s) 链接与站内绝对路径；javascript:/data: 等协议直接当作普通文本。 */
export function validTextLinkURL(url: string): boolean {
  if (/[\s\u0000-\u001f\u007f]/.test(url)) return false
  if (url.startsWith("https://") || url.startsWith("http://")) return true
  return url.startsWith("/") && !url.startsWith("//")
}

interface InlineMatch { index: number; length: number; span: WorkspaceTextInline }

const codePattern = /`([^`\n]+)`/
const linkPattern = /\[([^\]\n]+)\]\(([^)\s]+)\)/
const strongPattern = /\*\*([^*\n]+)\*\*/
const emPattern = /\*([^*\n]+)\*/

/** inlineSpans 把一行文本拆成纯文本与强调/代码/链接片段，链接协议不合法时按普通文本保留。 */
export function inlineSpans(content: string): WorkspaceTextInline[] {
  const spans: WorkspaceTextInline[] = []
  let rest = content
  while (rest.length > 0) {
    const match = nextInline(rest)
    if (!match) break
    if (match.index > 0) spans.push({ kind: "text", value: rest.slice(0, match.index) })
    spans.push(match.span)
    rest = rest.slice(match.index + match.length)
  }
  if (rest.length > 0) spans.push({ kind: "text", value: rest })
  return spans
}

function nextInline(value: string): InlineMatch | null {
  const candidates = [matchStrong(value), matchCode(value), matchLink(value), matchEm(value)].filter((match): match is InlineMatch => match !== null)
  if (candidates.length === 0) return null
  return candidates.reduce((left, right) => (right.index < left.index ? right : left))
}

function matchCode(value: string): InlineMatch | null {
  const match = codePattern.exec(value)
  return match ? { index: match.index, length: match[0].length, span: { kind: "code", value: match[1] } } : null
}

function matchStrong(value: string): InlineMatch | null {
  const match = strongPattern.exec(value)
  return match ? { index: match.index, length: match[0].length, span: { kind: "strong", value: match[1] } } : null
}

function matchEm(value: string): InlineMatch | null {
  const match = emPattern.exec(value)
  return match ? { index: match.index, length: match[0].length, span: { kind: "em", value: match[1] } } : null
}

function matchLink(value: string): InlineMatch | null {
  const match = linkPattern.exec(value)
  if (!match || !validTextLinkURL(match[2])) return null
  return { index: match.index, length: match[0].length, span: { kind: "link", value: match[1], url: match[2] } }
}

const headingPattern = /^(#{1,6})\s+(.*)$/
const unorderedPattern = /^[-*]\s+(.*)$/
const orderedPattern = /^\d+[.)]\s+(.*)$/
const fencePattern = /^```/
const delimiterCellPattern = /^(:)?-+(:)?$/

/** workspaceTextBlocks 把 Markdown 子集解析成块级结构；未知语法原样按段落渲染。 */
export function workspaceTextBlocks(source: string): WorkspaceTextBlock[] {
  const lines = source.split(/\r\n|\r|\n/)
  const blocks: WorkspaceTextBlock[] = []
  let index = 0
  while (index < lines.length) {
    const line = lines[index]!
    const trimmed = line.trim()
    if (!trimmed) { index += 1; continue }
    if (fencePattern.test(trimmed)) {
      const code: string[] = []
      index += 1
      while (index < lines.length && !fencePattern.test(lines[index]!.trim())) { code.push(lines[index]!); index += 1 }
      // 缺结束围栏时正好落在末尾，内容照样按代码块渲染。
      index += 1
      blocks.push({ kind: "code", value: code.join("\n") })
      continue
    }
    const heading = headingPattern.exec(trimmed)
    if (heading) { blocks.push({ kind: "heading", level: heading[1]!.length, spans: inlineSpans(heading[2]!.trim()) }); index += 1; continue }
    const aligns = tableAligns(lines[index + 1])
    if (aligns) {
      const headers = tableCells(trimmed).map(tableCellSpans)
      const raw: string[][] = []
      index += 2
      while (index < lines.length && lines[index]!.trim() && lines[index]!.includes("|")) { raw.push(tableCells(lines[index]!.trim())); index += 1 }
      blocks.push({ kind: "table", headers, aligns, rows: tableRows(raw) })
      continue
    }
    const listMatch = orderedPattern.exec(trimmed) ?? unorderedPattern.exec(trimmed)
    if (listMatch) {
      const ordered = orderedPattern.test(trimmed)
      const items: WorkspaceTextInline[][] = []
      while (index < lines.length) {
        const candidate = lines[index]!.trim()
        const item = ordered ? orderedPattern.exec(candidate) : unorderedPattern.exec(candidate)
        if (!item) break
        items.push(inlineSpans(item[1]!))
        index += 1
      }
      blocks.push({ kind: "list", ordered, items })
      continue
    }
    const paragraph: string[] = []
    while (index < lines.length && !blockStarter(lines, index)) { paragraph.push(lines[index]!.trim()); index += 1 }
    blocks.push({ kind: "paragraph", spans: inlineSpans(paragraph.join(" ")) })
  }
  return blocks
}

/** blockStarter 判断该行是否开启新的块；段落只吸收这些行之间的连续正文。 */
function blockStarter(lines: string[], index: number): boolean {
  const trimmed = lines[index]!.trim()
  if (!trimmed) return true
  if (fencePattern.test(trimmed) || headingPattern.test(trimmed)) return true
  if (unorderedPattern.test(trimmed) || orderedPattern.test(trimmed)) return true
  return tableAligns(lines[index + 1]) !== null
}

/** tableAligns 解析 Markdown 表格的分隔行（`| --- | :---: |`），至少要有两列；
 *  返回 null 表示这一行不是分隔行，返回数组则按列给出对齐（没有冒号时是 null，交给表格默认样式）。 */
export function tableAligns(line: string | undefined): WorkspaceTextAlign[] | null {
  if (line === undefined) return null
  const trimmed = line.trim()
  if (!trimmed.includes("|") || !trimmed.includes("-")) return null
  const cells = tableCells(trimmed)
  if (cells.length < 2) return null
  const aligns: WorkspaceTextAlign[] = []
  for (const cell of cells) {
    const match = delimiterCellPattern.exec(cell)
    if (!match) return null
    aligns.push(match[1] && match[2] ? "center" : match[1] ? "left" : match[2] ? "right" : null)
  }
  return aligns
}

/** `<br>`（含 `</br>`、`<br/>` 写法）在表格单元格里换行：Markdown 表格的单元格压在一行内，
 *  这是 Grafana 文本面板里写多行阈值的常用写法；HTML 其余部分仍按普通文本转义。 */
const tableBreakPattern = /<br\s*\/?>|<\/br>/i

export function tableCellSpans(cell: string): WorkspaceTextInline[] {
  const segments = cell.split(tableBreakPattern)
  const spans: WorkspaceTextInline[] = []
  for (const [index, segment] of segments.entries()) {
    if (index > 0) spans.push({ kind: "break", value: "" })
    spans.push(...inlineSpans(segment))
  }
  return spans.length > 0 ? spans : [{ kind: "text", value: "" }]
}

/** 表格合并标记：整格只有 `>>` 时并入左侧格（colspan），只有 `^^` 时并入上方格（rowspan）；
 *  其它位置出现这两个符号都按普通文本渲染，写错位置时作者能直接看见。表头不参与合并。 */
const mergeLeftMarker = ">>"
const mergeUpMarker = "^^"

/**
 * tableRows 把原始单元格合成带跨度的行。合并目标必须是同一行的左侧锚格或同一列的上一行锚格；
 * 找不到目标时标记原样保留为文本。被合并的格子从行里移除，由 HTML 表格按跨度自动占位。
 */
export function tableRows(raw: string[][]): WorkspaceTextTableCell[][] {
  const rows: WorkspaceTextTableCell[][] = []
  for (const cells of raw) {
    const anchors: WorkspaceTextTableCell[] = []
    for (const [column, cell] of cells.entries()) {
      const left = [...anchors].reverse().find((candidate) => candidate.column < column)
      if (cell === mergeLeftMarker && left) { left.colSpan += 1; continue }
      const above = [...(rows.at(-1) ?? [])].reverse().find((candidate) => candidate.column === column)
      if (cell === mergeUpMarker && above) { above.rowSpan += 1; continue }
      anchors.push({ column, spans: tableCellSpans(cell), rowSpan: 1, colSpan: 1 })
    }
    rows.push(anchors)
  }
  return rows
}

/** tableCells 去掉首尾的竖线后按 `|` 切分；单元格内容由 inlineSpans 继续解析。 */
function tableCells(line: string): string[] {
  return line.replace(/^\|/, "").replace(/\|$/, "").split("|").map((cell) => cell.trim())
}

/** textPatch 在面板类型切换时清理文本内容，避免保存出非文本面板带 text 的无效配置。 */
export function textPatch(type: string): Pick<DashboardPanel, "text"> {
  return type === "text" ? {} : { text: undefined }
}

/** 文本面板只保留标题、尺寸、Markdown 与数据链接，其余查询/可视化字段必须清空。 */
export function emptyTextPanel(panel: DashboardPanel): DashboardPanel {
  const text = panel.text ?? ""
  return { id: panel.id, title: panel.title, type: "text", size: panel.size, legend: false, thresholds: [], series: [],
    ...(text ? { text } : {}), ...(panel.links?.length ? { links: panel.links } : {}) }
}
