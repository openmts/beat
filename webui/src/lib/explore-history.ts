// 指标探索的查询历史：与 Grafana Explore 一样只存在本地浏览器，不上传服务端。
import { exploreSearch, type ExploreState } from "@/lib/metric-explore"

export interface ExploreHistoryEntry {
  /** 规范化的探索状态编码，恢复时直接交给 parseExploreState。 */
  search: string
  query_type: "range" | "instant"
  /** 可读摘要，只用于列表展示；恢复始终以 search 为准。 */
  summary: string
  ran_at: string
  starred: boolean
}

export const exploreHistoryStorageKey = "beat.explore.history.v1"
export const exploreHistoryLimit = 50
const maximumSummaryLength = 200

/** 读取历史：任何损坏或越界的持久化内容都退化成空历史，不影响探索页可用性。 */
export function loadExploreHistory(storage: Storage | undefined): ExploreHistoryEntry[] {
  if (!storage) return []
  try {
    const raw = storage.getItem(exploreHistoryStorageKey)
    if (!raw) return []
    return normalizeHistory(JSON.parse(raw))
  } catch {
    return []
  }
}

export function saveExploreHistory(storage: Storage | undefined, entries: ExploreHistoryEntry[]): void {
  if (!storage) return
  try { storage.setItem(exploreHistoryStorageKey, JSON.stringify(entries)) } catch { /* 存储被禁用或已满时只丢历史 */ }
}

/** 记录一次执行：同一查询只保留最新一条并置顶，总条数按上限裁剪。 */
export function recordExploreHistory(entries: ExploreHistoryEntry[], state: ExploreState, ranAt: Date): ExploreHistoryEntry[] {
  const search = exploreSearch(state)
  const existing = entries.find((entry) => entry.search === search)
  const entry: ExploreHistoryEntry = {
    search,
    query_type: state.query_type,
    summary: exploreHistorySummary(state),
    ran_at: ranAt.toISOString(),
    starred: existing?.starred ?? false,
  }
  return [entry, ...entries.filter((candidate) => candidate.search !== search)].slice(0, exploreHistoryLimit)
}

export function toggleExploreHistoryStar(entries: ExploreHistoryEntry[], search: string, starred: boolean): ExploreHistoryEntry[] {
  return entries.map((entry) => (entry.search === search ? { ...entry, starred } : entry))
}

/** 删除选中的若干条历史：只按 search 移除，星标与其它条目保持不变。 */
export function removeExploreHistory(entries: ExploreHistoryEntry[], searches: string[]): ExploreHistoryEntry[] {
  if (searches.length === 0) return entries
  const removing = new Set(searches)
  return entries.filter((entry) => !removing.has(entry.search))
}

/**
 * 键盘导航的下一步：在筛选结果里按上下键循环移动，Home/End 跳到首尾。
 * 空列表返回 -1（没有高亮行），与「未开始导航」的初值一致。
 */
export function nextExploreHistoryIndex(count: number, current: number, key: "ArrowDown" | "ArrowUp" | "Home" | "End"): number {
  if (count <= 0) return -1
  if (key === "Home") return 0
  if (key === "End") return count - 1
  if (current < 0) return key === "ArrowDown" ? 0 : count - 1
  return (current + (key === "ArrowDown" ? 1 : -1) + count) % count
}

/** Grafana 的「Clear history」同样保留星标条目，避免误删重点查询。 */
export function clearExploreHistory(entries: ExploreHistoryEntry[]): ExploreHistoryEntry[] {
  return entries.filter((entry) => entry.starred)
}

export function filterExploreHistory(entries: ExploreHistoryEntry[], needle: string, options?: { starredOnly?: boolean }): ExploreHistoryEntry[] {
  const value = needle.trim().toLowerCase()
  return entries.filter((entry) => {
    if (options?.starredOnly && !entry.starred) return false
    if (value === "") return true
    return entry.summary.toLowerCase().includes(value) || entry.search.toLowerCase().includes(value)
  })
}

/** 列表排序：Grafana 的 Explore 历史同样提供「最近/最早」。并列时保持原顺序（Array#sort 稳定），
 *  因此同一次执行的条目不会被随机调换。 */
export type ExploreHistoryOrder = "newest" | "oldest"

export function sortExploreHistory(entries: ExploreHistoryEntry[], order: ExploreHistoryOrder): ExploreHistoryEntry[] {
  const direction = order === "oldest" ? 1 : -1
  return entries.slice().sort((left, right) => direction * (exploreHistoryTimestamp(left) - exploreHistoryTimestamp(right)))
}

function exploreHistoryTimestamp(entry: ExploreHistoryEntry): number {
  const value = Date.parse(entry.ran_at)
  return Number.isFinite(value) ? value : 0
}

/** 摘要按「指标·聚合」逐行拼接，不依赖目录与节点，历史在节点改名后仍然可读。 */
export function exploreHistorySummary(state: ExploreState): string {
  const rows = state.queries.filter((query) => !query.hidden && query.metric !== "").map((query) => `${query.metric} · ${query.aggregation}`)
  if (rows.length === 0) return ""
  return rows.join(" + ").slice(0, maximumSummaryLength)
}

function normalizeHistory(value: unknown): ExploreHistoryEntry[] {
  if (!Array.isArray(value)) return []
  const entries: ExploreHistoryEntry[] = []
  for (const item of value) {
    const entry = normalizeEntry(item)
    if (entry && !entries.some((existing) => existing.search === entry.search)) entries.push(entry)
    if (entries.length >= exploreHistoryLimit) break
  }
  return entries
}

function normalizeEntry(value: unknown): ExploreHistoryEntry | null {
  if (typeof value !== "object" || value === null) return null
  const candidate = value as Partial<Record<keyof ExploreHistoryEntry, unknown>>
  if (typeof candidate.search !== "string" || candidate.search === "" || candidate.search.length > 4096) return null
  if (typeof candidate.summary !== "string" || candidate.summary.length > maximumSummaryLength) return null
  if (typeof candidate.ran_at !== "string" || !Number.isFinite(Date.parse(candidate.ran_at))) return null
  if (candidate.query_type !== "range" && candidate.query_type !== "instant") return null
  return { search: candidate.search, summary: candidate.summary, ran_at: candidate.ran_at, query_type: candidate.query_type, starred: candidate.starred === true }
}

/** localStorage 在隐私模式或禁用存储时会抛错；拿不到就按「无历史」处理。 */
export function exploreHistoryStorage(): Storage | undefined {
  try {
    return typeof window === "undefined" ? undefined : window.localStorage
  } catch {
    return undefined
  }
}
