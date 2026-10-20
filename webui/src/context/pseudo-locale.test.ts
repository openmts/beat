import { describe, expect, it } from "vitest"
import { shippedLocales, translateFrom } from "@/context/locales"
import { allCatalogs } from "@/test/catalogs"
import { pseudoCarrierLocale, pseudoCatalog, pseudoCatalogs, pseudoLocaleMarker, pseudoValue } from "@/test/pseudo-locale"

const english: Record<string, string> = {}
for (const catalog of allCatalogs) Object.assign(english, catalog.en ?? {})

function placeholders(value: string): string[] {
  return [...value.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort()
}

// 伪语言溢出检查的离线部分：生成一份“更长的目标语言”，验证占位符、长度上界与界定符，
// 供宿主机浏览器演练直接装载；真实 DOM 溢出断言需要浏览器，仍属宿主证据。
describe("pseudo locale", () => {
  it("covers every source key and stays distinguishable from real locales", () => {
    const pseudo = pseudoCatalog(english)
    expect(Object.keys(pseudo).sort()).toEqual(Object.keys(english).sort())
    expect(Object.keys(pseudo).length).toBeGreaterThan(1000)
    expect(shippedLocales).not.toContain(pseudoCarrierLocale)
    expect(pseudoValue("Nodes")).toBe(`${pseudoLocaleMarker[0]}Nodesxx${pseudoLocaleMarker[1]}`)
    expect(pseudoValue("{count} nodes")).toBe(`${pseudoLocaleMarker[0]}{count} nodesxx${pseudoLocaleMarker[1]}`)
  })

  it("keeps placeholders, expands text and stays inside the catalog byte budget", () => {
    const pseudo = pseudoCatalog(english)
    const problems: string[] = []
    for (const [key, value] of Object.entries(pseudo)) {
      const reference = english[key]
      if (JSON.stringify(placeholders(value)) !== JSON.stringify(placeholders(reference))) problems.push(`${key}: placeholder mismatch`)
      if (new TextEncoder().encode(value).length > 2000) problems.push(`${key}: exceeds 2000 bytes`)
      if (/[A-Za-z]/.test(reference) && value.length <= reference.length) problems.push(`${key}: not expanded`)
      if (value.includes("\u0000")) problems.push(`${key}: control character`)
    }
    expect(problems).toEqual([])
  })

  it("renders through the same lookup used by the application", () => {
    const catalogs = pseudoCatalogs(english)
    expect(translateFrom(catalogs, pseudoCarrierLocale, "app.nodes")).toBe(pseudoValue(english["app.nodes"]))
    expect(translateFrom(catalogs, "en", "app.nodes")).toBe("app.nodes")
  })
})
