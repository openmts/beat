import { describe, expect, it } from "vitest"
import { inlineSpans, maximumTextBytes, tableAligns, tableCellSpans, textPanelError, validTextLinkURL, validWorkspaceText, workspaceTextBlocks } from "@/lib/workspace-text"
import type { DashboardPanel } from "@/types/metric-workspace"

const cell = (column: number, spans: ReturnType<typeof tableCellSpans>, rowSpan = 1, colSpan = 1) => ({ column, spans, rowSpan, colSpan })

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "Notes", type: "text", size: "medium", legend: false, thresholds: [], series: [], ...patch })

describe("workspace text validation", () => {
  it("accepts markdown、空内容与 CRLF，并拒绝超长文本", () => {
    expect(validWorkspaceText("# 就能说明\r\n\r\n- 第一项")).toBe(true)
    expect(validWorkspaceText("")).toBe(true)
    expect(validWorkspaceText("a".repeat(maximumTextBytes))).toBe(true)
    expect(validWorkspaceText("a".repeat(maximumTextBytes + 1))).toBe(false)
  })

  it("rejects control characters other than newline, carriage return and tab", () => {
    expect(validWorkspaceText("正常\t文本\n换行")).toBe(true)
    expect(validWorkspaceText(`前缀${String.fromCharCode(0)}后缀`)).toBe(false)
    expect(validWorkspaceText(`前缀${String.fromCharCode(0x1f)}后缀`)).toBe(false)
    expect(validWorkspaceText(`前缀${String.fromCharCode(0x7f)}后缀`)).toBe(false)
  })

  it("only allows text content on text panels", () => {
    expect(textPanelError(panel({ text: "ok" }))).toBeNull()
    expect(textPanelError(panel({}))).toBeNull()
    expect(textPanelError(panel({ text: "x".repeat(maximumTextBytes + 1) }))).toBe("workspace.text_invalid")
    expect(textPanelError({ ...panel({ text: undefined }), type: "timeseries" })).toBeNull()
    expect(textPanelError({ ...panel({ text: "stray" }), type: "timeseries" })).toBe("workspace.text_invalid")
  })

  it("accepts only absolute http(s) and site paths as links", () => {
    expect(validTextLinkURL("https://grafana.example/runbook")).toBe(true)
    expect(validTextLinkURL("http://10.0.0.1:9090")).toBe(true)
    expect(validTextLinkURL("/admin/metrics?node_id=n1")).toBe(true)
    expect(validTextLinkURL("//evil.test/steal")).toBe(false)
    expect(validTextLinkURL("javascript:alert(1)")).toBe(false)
    expect(validTextLinkURL("data:text/html;base64,PHNjcmlwdD4=")).toBe(false)
    expect(validTextLinkURL("/空格 路径")).toBe(false)
  })
})

describe("workspace text inline parsing", () => {
  it("splits code、bold、italic 与链接", () => {
    expect(inlineSpans("查看 `uptime` 与 **阈值** 和 *说明*")).toEqual([
      { kind: "text", value: "查看 " }, { kind: "code", value: "uptime" }, { kind: "text", value: " 与 " },
      { kind: "strong", value: "阈值" }, { kind: "text", value: " 和 " }, { kind: "em", value: "说明" },
    ])
    expect(inlineSpans("[运行手册](/admin/runbook)")).toEqual([{ kind: "link", value: "运行手册", url: "/admin/runbook" }])
  })

  it("keeps unsafe link protocols as plain text", () => {
    expect(inlineSpans("[点我](javascript:alert(1))")).toEqual([{ kind: "text", value: "[点我](javascript:alert(1))" }])
    expect(inlineSpans("[点我](data:text/html,<script>alert(1)</script>)")).toEqual([
      { kind: "text", value: "[点我](data:text/html,<script>alert(1)</script>)" }]
    )
  })

  it("leaves raw HTML and bare markup untouched", () => {
    expect(inlineSpans("<script>alert(1)</script>")).toEqual([{ kind: "text", value: "<script>alert(1)</script>" }])
    expect(inlineSpans("2 * 3 = 6")).toEqual([{ kind: "text", value: "2 * 3 = 6" }])
  })
})

describe("workspace text blocks", () => {
  it("parses headings、lists、code fences 与段落", () => {
    const source = ["# 标题", "", "第一行", "第二行", "", "- 甲", "- 乙", "", "1. 一", "2. 二", "", "```", "df -h", "```"].join("\n")
    expect(workspaceTextBlocks(source)).toEqual([
      { kind: "heading", level: 1, spans: [{ kind: "text", value: "标题" }] },
      { kind: "paragraph", spans: [{ kind: "text", value: "第一行 第二行" }] },
      { kind: "list", ordered: false, items: [[{ kind: "text", value: "甲" }], [{ kind: "text", value: "乙" }]] },
      { kind: "list", ordered: true, items: [[{ kind: "text", value: "一" }], [{ kind: "text", value: "二" }]] },
      { kind: "code", value: "df -h" },
    ])
  })

  it("parses markdown tables with inline formatting in every cell", () => {
    const source = ["| 阈值 | 处置 |", "| --- | :---: |", "| \u2265 80% | **扩容** |", "| `0` | *观察* |"].join("\n")
    expect(workspaceTextBlocks(source)).toEqual([
      { kind: "table", aligns: [null, "center"],
        headers: [[{ kind: "text", value: "阈值" }], [{ kind: "text", value: "处置" }]],
        rows: [
          [cell(0, [{ kind: "text", value: "≥ 80%" }]), cell(1, [{ kind: "strong", value: "扩容" }])],
          [cell(0, [{ kind: "code", value: "0" }]), cell(1, [{ kind: "em", value: "观察" }])],
        ] },
    ])
  })

  it("reads left, center and right column alignment from the delimiter row", () => {
    expect(tableAligns("| :--- | :---: | ---: | --- |")).toEqual(["left", "center", "right", null])
    expect(tableAligns("  |:--|--:|  ")).toEqual(["left", "right"])
    expect(tableAligns("| --- |")).toBeNull()
    expect(tableAligns("| --- | text |")).toBeNull()
    expect(tableAligns(undefined)).toBeNull()
    expect(tableAligns("---")).toBeNull()
  })

  it("applies the alignment to the table block", () => {
    const blocks = workspaceTextBlocks("| 名称 | 数值 | 备注 |\n| :--- | ---: | :---: |\n| cpu | 42 | ok |")
    expect(blocks).toHaveLength(1)
    expect(blocks[0]).toMatchObject({ kind: "table", aligns: ["left", "right", "center"] })
  })

  it("keeps a table-looking line as a paragraph without a delimiter row", () => {
    expect(workspaceTextBlocks("| 一行 | 两列 |")).toEqual([
      { kind: "paragraph", spans: [{ kind: "text", value: "| 一行 | 两列 |" }] },
    ])
  })

  it("closes an unterminated code fence at the end of the source", () => {
    expect(workspaceTextBlocks("前言\n```\ntop -b")).toEqual([
      { kind: "paragraph", spans: [{ kind: "text", value: "前言" }] },
      { kind: "code", value: "top -b" },
    ])
  })

  it("returns nothing for empty content", () => {
    expect(workspaceTextBlocks("")).toEqual([])
    expect(workspaceTextBlocks("\n\n")).toEqual([])
  })
})

describe("table cell line breaks", () => {
  it("turns the three <br> spellings into break spans between inline spans", () => {
    expect(tableCellSpans("cpu<br>memory")).toEqual([{ kind: "text", value: "cpu" }, { kind: "break", value: "" }, { kind: "text", value: "memory" }])
    expect(tableCellSpans("阈值<br/>**说明**")).toEqual([{ kind: "text", value: "阈值" }, { kind: "break", value: "" }, { kind: "strong", value: "说明" }])
    expect(tableCellSpans("a</br>b<BR />c")).toEqual([
      { kind: "text", value: "a" }, { kind: "break", value: "" }, { kind: "text", value: "b" },
      { kind: "break", value: "" }, { kind: "text", value: "c" }])
  })

  it("keeps the parser on plain text for every other tag and for empty cells", () => {
    expect(tableCellSpans("<script>alert(1)</script>")).toEqual([{ kind: "text", value: "<script>alert(1)</script>" }])
    expect(tableCellSpans("")).toEqual([{ kind: "text", value: "" }])
  })

  it("merges a cell into the one above with ^^ and into the one on the left with >>", () => {
    const blocks = workspaceTextBlocks(["| 设备 | 指标 | 值 |", "| --- | --- | --- |", "| node-a | cpu | 40 |", "| ^^ | memory | 20 |", "| node-b | disk | >> |"].join("\n"))
    const table = blocks[0]
    expect(table?.kind).toBe("table")
    const rows = table?.kind === "table" ? table.rows : []
    expect(rows[0]).toEqual([cell(0, [{ kind: "text", value: "node-a" }], 2), cell(1, [{ kind: "text", value: "cpu" }]), cell(2, [{ kind: "text", value: "40" }])])
    expect(rows[1]).toEqual([cell(1, [{ kind: "text", value: "memory" }]), cell(2, [{ kind: "text", value: "20" }])])
    expect(rows[2]).toEqual([cell(0, [{ kind: "text", value: "node-b" }]), cell(1, [{ kind: "text", value: "disk" }], 1, 2)])
  })

  it("keeps merge markers as text when there is nothing to merge with", () => {
    const blocks = workspaceTextBlocks(["| 设备 | 指标 |", "| --- | --- |", "| >> | ^^ |"].join("\n"))
    const table = blocks[0]
    const rows = table?.kind === "table" ? table.rows : []
    expect(rows[0]).toEqual([cell(0, [{ kind: "text", value: ">>" }]), cell(1, [{ kind: "text", value: "^^" }])])
  })

  it("applies breaks inside table headers and body cells", () => {
    const blocks = workspaceTextBlocks("| 指标<br>聚合 | 值 |\n| --- | --- |\n| cpu<br/>avg | 42 |")
    expect(blocks).toEqual([{ kind: "table", aligns: [null, null], rows: [[
      cell(0, [{ kind: "text", value: "cpu" }, { kind: "break", value: "" }, { kind: "text", value: "avg" }]), cell(1, [{ kind: "text", value: "42" }])]],
      headers: [[{ kind: "text", value: "指标" }, { kind: "break", value: "" }, { kind: "text", value: "聚合" }], [{ kind: "text", value: "值" }]] }])
  })
})
