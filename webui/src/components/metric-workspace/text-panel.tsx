import type { ReactNode } from "react"
import { panelLinks, resolvePanelLinkURL } from "@/lib/workspace-links"
import { WorkspaceLinkIcon } from "./link-icon"
import { workspaceTextBlocks, type WorkspaceTextAlign, type WorkspaceTextInline, type WorkspaceTextTableCell } from "@/lib/workspace-text"
import type { DashboardPanel } from "@/types/metric-workspace"

/** 文本面板正文：Grafana 的 Text panel 只渲染 Markdown 子集，不查询数据也不显示状态行。 */
export function WorkspaceTextPanel(props: { panel: DashboardPanel; linkValues?: Record<string, string>; hideLinks?: boolean }) {
  const blocks = workspaceTextBlocks(props.panel.text ?? "")
  const links = props.hideLinks ? [] : panelLinks(props.panel)
  // 正文里的 `${...}` 与面板链接共用一套取值：文本面板写 runbook 链接时同样能带上变量与时间范围。
  const values = props.linkValues ?? {}
  return <div data-workspace-text className="min-w-0 space-y-3 px-4 py-3">
    {blocks.map((block, index) => {
      if (block.kind === "code") return <pre key={index} className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">{block.value}</pre>
      if (block.kind === "list") return <ListBlock key={index} ordered={block.ordered} items={block.items} values={values} />
      if (block.kind === "table") return <TableBlock key={index} headers={block.headers} aligns={block.aligns} rows={block.rows} values={values} />
      if (block.kind === "heading") return <HeadingBlock key={index} level={block.level} spans={block.spans} values={values} />
      return <p key={index} className="text-sm leading-relaxed">{spans(block.spans, values)}</p>
    })}
    {links.length > 0 && <div data-workspace-links className="flex flex-wrap items-center gap-3 pt-1">
      {links.map((link, index) => <a key={index} href={resolvePanelLinkURL(link.url, values)} target={link.new_tab ? "_blank" : undefined} rel={link.new_tab ? "noopener noreferrer" : undefined}
        className="inline-flex items-center gap-1 text-xs text-primary underline-offset-4 hover:underline"><WorkspaceLinkIcon link={link} />{link.title}</a>)}
    </div>}
  </div>
}

function ListBlock(props: { ordered: boolean; items: WorkspaceTextInline[][]; values: Record<string, string> }) {
  const className = "list-inside space-y-1 pl-1 text-sm leading-relaxed " + (props.ordered ? "list-decimal" : "list-disc")
  const entries = props.items.map((item, index) => <li key={index}>{spans(item, props.values)}</li>)
  return props.ordered ? <ol className={className}>{entries}</ol> : <ul className={className}>{entries}</ul>
}

/** Markdown 表格：列宽自适应并可横向滚动，表头与单元格共用同一套行内渲染；
 *  分隔行里的冒号决定该列的对齐，没有冒号的列沿用表格默认的左对齐。 */
function TableBlock(props: { headers: WorkspaceTextInline[][]; aligns: WorkspaceTextAlign[]; rows: WorkspaceTextTableCell[][]; values: Record<string, string> }) {
  const style = (index: number) => props.aligns[index] ? { textAlign: props.aligns[index]! } : undefined
  return <div className="overflow-x-auto">
    <table className="w-full text-left text-xs">
      <thead><tr className="border-b">{props.headers.map((header, index) => <th key={index} style={style(index)} className="py-2 pr-4 font-medium">{spans(header, props.values)}</th>)}</tr></thead>
      <tbody>{props.rows.map((row, rowIndex) => <tr key={rowIndex} className="border-b last:border-0">
        {row.map((cell) => <td key={cell.column} style={style(cell.column)} rowSpan={cell.rowSpan > 1 ? cell.rowSpan : undefined}
          colSpan={cell.colSpan > 1 ? cell.colSpan : undefined} className="py-2 pr-4 align-top">{spans(cell.spans, props.values)}</td>)}
      </tr>)}</tbody>
    </table>
  </div>
}

/** 面板里的标题降级到 h3-h5，避免一个面板把整页标题层级打乱。 */
function HeadingBlock(props: { level: number; spans: WorkspaceTextInline[]; values: Record<string, string> }) {
  const className = props.level <= 1 ? "text-base font-semibold" : props.level === 2 ? "text-sm font-semibold" : "text-sm font-medium"
  if (props.level <= 1) return <h3 className={className}>{spans(props.spans, props.values)}</h3>
  if (props.level === 2) return <h4 className={className}>{spans(props.spans, props.values)}</h4>
  return <h5 className={className}>{spans(props.spans, props.values)}</h5>
}

/** span 渲染：文本作为 React 子节点输出，因此 Markdown 里的 HTML 会被转义而不是执行。 */
function spans(items: WorkspaceTextInline[], values: Record<string, string>): ReactNode[] {
  return items.map((span, index) => {
    if (span.kind === "code") return <code key={index} className="rounded bg-muted px-1 py-0.5 font-mono text-xs">{span.value}</code>
    if (span.kind === "strong") return <strong key={index}>{span.value}</strong>
    if (span.kind === "em") return <em key={index}>{span.value}</em>
    if (span.kind === "link") return <a key={index} href={resolvePanelLinkURL(span.url, values)} target="_blank" rel="noopener noreferrer" className="text-primary underline-offset-4 hover:underline">{span.value}</a>
    if (span.kind === "break") return <br key={index} />
    return <span key={index}>{span.value}</span>
  })
}
