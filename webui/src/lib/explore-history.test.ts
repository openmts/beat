import { describe, expect, it } from "vitest"
import { clearExploreHistory, exploreHistoryLimit, exploreHistoryStorage, filterExploreHistory, loadExploreHistory, nextExploreHistoryIndex, recordExploreHistory, removeExploreHistory, saveExploreHistory, sortExploreHistory, toggleExploreHistoryStar, type ExploreHistoryEntry } from "@/lib/explore-history"
import { exploreSearch, type ExploreState } from "@/lib/metric-explore"

function state(overrides: Partial<ExploreState> = {}): ExploreState {
  return {
    queries: [{ ref_id: "A", metric: "cpu_usage", aggregation: "avg", node_id: "node-a", device_id: "", color: "blue", hidden: false }],
    range: { mode: "relative", seconds: 3600 },
    max_points: 300,
    query_type: "range",
    ...overrides,
  }
}

function memoryStorage(): Storage {
  const values = new Map<string, string>()
  return {
    get length() { return values.size },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => { values.delete(key) },
    setItem: (key, value) => { values.set(key, value) },
  }
}

describe("explore history", () => {
  it("deduplicates by canonical search and keeps the newest first", () => {
    const first = recordExploreHistory([], state(), new Date("2026-10-17T01:00:00Z"))
    expect(first).toHaveLength(1)
    expect(first[0]!.summary).toBe("cpu_usage · avg")
    expect(first[0]!.query_type).toBe("range")

    const second = recordExploreHistory(first, state({ query_type: "instant" }), new Date("2026-10-17T02:00:00Z"))
    expect(second).toHaveLength(2)
    expect(second[0]!.query_type).toBe("instant")
    expect(second[0]!.ran_at).toBe("2026-10-17T02:00:00.000Z")

    const repeated = recordExploreHistory(second, state({ query_type: "instant" }), new Date("2026-10-17T03:00:00Z"))
    expect(repeated).toHaveLength(2)
    expect(repeated[0]!.ran_at).toBe("2026-10-17T03:00:00.000Z")
    expect(repeated[0]!.search).toBe(exploreSearch(state({ query_type: "instant" })))
  })

  it("caps the stored entries at the history limit", () => {
    let entries = recordExploreHistory([], state(), new Date())
    for (let index = 0; index < exploreHistoryLimit + 5; index += 1) {
      entries = recordExploreHistory(entries, state({ max_points: 60 + index }), new Date())
    }
    expect(exploreHistoryLimit).toBe(50)
    expect(entries).toHaveLength(exploreHistoryLimit)
  })

  it("keeps starred entries when clearing and reports their star state", () => {
    const recorded = recordExploreHistory(recordExploreHistory([], state(), new Date()), state({ max_points: 60 }), new Date())
    const starred = toggleExploreHistoryStar(recorded, recorded[1]!.search, true)
    expect(starred[1]!.starred).toBe(true)
    expect(toggleExploreHistoryStar(starred, recorded[1]!.search, false)[1]!.starred).toBe(false)

    const cleared = clearExploreHistory(starred)
    expect(cleared).toHaveLength(1)
    expect(cleared[0]!.starred).toBe(true)
  })

  it("filters by summary and canonical search text", () => {
    const entries = recordExploreHistory(recordExploreHistory([], state(), new Date()), state({ max_points: 600 }), new Date())
    expect(filterExploreHistory(entries, "")).toHaveLength(2)
    expect(filterExploreHistory(entries, "CPU_USAGE")).toHaveLength(2)
    expect(filterExploreHistory(entries, "node-a")).toHaveLength(2)
    expect(filterExploreHistory(entries, "disk")).toEqual([])
  })

  it("round-trips through storage and rejects corrupt payloads", () => {
    const storage = memoryStorage()
    expect(loadExploreHistory(storage)).toEqual([])
    const entries = recordExploreHistory([], state(), new Date("2026-10-17T01:00:00Z"))
    saveExploreHistory(storage, entries)
    expect(loadExploreHistory(storage)).toEqual(entries)

    storage.setItem("beat.explore.history.v1", "{not json")
    expect(loadExploreHistory(storage)).toEqual([])

    storage.setItem("beat.explore.history.v1", JSON.stringify([
      { search: "a=1", summary: "ok", ran_at: "2026-10-17T01:00:00Z", query_type: "range", starred: true },
      { search: "", summary: "missing search", ran_at: "2026-10-17T01:00:00Z", query_type: "range" },
      { search: "b=2", summary: "bad type", ran_at: "2026-10-17T01:00:00Z", query_type: "week" },
      { search: "c=3", summary: "bad date", ran_at: "nope", query_type: "range" },
      { search: "a=1", summary: "duplicate", ran_at: "2026-10-17T01:00:00Z", query_type: "range" },
      "not an object",
    ]))
    expect(loadExploreHistory(storage)).toEqual([{ search: "a=1", summary: "ok", ran_at: "2026-10-17T01:00:00Z", query_type: "range", starred: true }])
    expect(loadExploreHistory(undefined)).toEqual([])
  })

  it("removes only the selected entries", () => {
    const entries = [recordExploreHistory([], state(), new Date("2026-10-17T01:00:00Z"))[0]!,
      recordExploreHistory([], state({ query_type: "instant", max_points: undefined }), new Date("2026-10-17T02:00:00Z"))[0]!]
    expect(removeExploreHistory(entries, [])).toHaveLength(2)
    expect(removeExploreHistory(entries, [entries[1]!.search]).map((entry) => entry.ran_at)).toEqual([entries[0]!.ran_at])
  })

  it("wraps keyboard navigation and clamps to the filtered list", () => {
    expect(nextExploreHistoryIndex(0, -1, "ArrowDown")).toBe(-1)
    expect(nextExploreHistoryIndex(3, -1, "ArrowDown")).toBe(0)
    expect(nextExploreHistoryIndex(3, -1, "ArrowUp")).toBe(2)
    expect(nextExploreHistoryIndex(3, 2, "ArrowDown")).toBe(0)
    expect(nextExploreHistoryIndex(3, 0, "ArrowUp")).toBe(2)
    expect(nextExploreHistoryIndex(3, 1, "Home")).toBe(0)
    expect(nextExploreHistoryIndex(3, 1, "End")).toBe(2)
  })

  it("reports no storage instead of throwing outside a browser", () => {
    expect(exploreHistoryStorage()).not.toBeUndefined()
  })
})

describe("history sorting and star filter", () => {
  const entry = (summary: string, ranAt: string, starred = false): ExploreHistoryEntry =>
    ({ search: `q=${summary}`, query_type: "range", summary, ran_at: ranAt, starred })

  it("orders by run time in both directions without mutating the input", () => {
    const entries = [entry("b", "2026-10-17T02:00:00.000Z"), entry("c", "2026-10-17T03:00:00.000Z"), entry("a", "2026-10-17T01:00:00.000Z")]
    expect(sortExploreHistory(entries, "newest").map((value) => value.summary)).toEqual(["c", "b", "a"])
    expect(sortExploreHistory(entries, "oldest").map((value) => value.summary)).toEqual(["a", "b", "c"])
    expect(entries.map((value) => value.summary)).toEqual(["b", "c", "a"])
  })

  it("keeps unparsable run times last when the newest query is on top", () => {
    const entries = [entry("broken", "not-a-date"), entry("recent", "2026-10-17T03:00:00.000Z")]
    expect(sortExploreHistory(entries, "newest").map((value) => value.summary)).toEqual(["recent", "broken"])
  })

  it("combines the search text with the starred-only filter", () => {
    const entries = [entry("cpu avg", "2026-10-17T01:00:00.000Z", true), entry("cpu max", "2026-10-17T02:00:00.000Z"), entry("memory avg", "2026-10-17T03:00:00.000Z", true)]
    expect(filterExploreHistory(entries, "", { starredOnly: true }).map((value) => value.summary)).toEqual(["cpu avg", "memory avg"])
    expect(filterExploreHistory(entries, "max", { starredOnly: true })).toEqual([])
    expect(filterExploreHistory(entries, "max", { starredOnly: false }).map((value) => value.summary)).toEqual(["cpu max"])
    expect(filterExploreHistory(entries, "")).toEqual(entries)
  })
})
