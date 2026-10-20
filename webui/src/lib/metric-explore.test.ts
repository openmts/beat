import { describe, expect, it } from "vitest"
import { emptyMetricCatalog, exploreDefaultState, exploreInstantSupported, exploreLatestRequest, exploreSearch, parseExploreState, type ExploreState } from "./metric-explore"
import { workspaceSources } from "@/test/metric-workspace-fixtures"

function state(): ExploreState {
  const sources = workspaceSources()
  return exploreDefaultState(sources.catalog, sources.nodes)
}

describe("metric explore instant queries", () => {
  it("builds a bounded instant payload and dedupes repeated node and metric pairs", () => {
    const value = state()
    value.query_type = "instant"
    value.queries = [value.queries[0], { ...value.queries[0], ref_id: "B" }, { ...value.queries[0], ref_id: "C", metric: "memory" }]
    expect(exploreLatestRequest(value)).toEqual({ series: [
      { ref_id: "A", node_id: "node-one", metric: "cpu" },
      { ref_id: "C", node_id: "node-one", metric: "memory" },
    ] })
    expect(exploreInstantSupported(value)).toBe(true)
    value.queries[0] = { ...value.queries[0], device_id: "gpu0" }
    expect(exploreInstantSupported(value)).toBe(false)
    expect(() => exploreLatestRequest({ ...value, queries: [] })).toThrow("explore.no_queries")
  })

  it("keeps the query type in the URL and defaults unknown values to range", () => {
    const value = state()
    expect(exploreSearch(value)).not.toContain("type=")
    value.query_type = "instant"
    expect(exploreSearch(value)).toContain("type=instant")
    const sources = workspaceSources()
    expect(parseExploreState(exploreSearch(value), sources.catalog, sources.nodes).query_type).toBe("instant")
    expect(parseExploreState("type=unknown", sources.catalog, sources.nodes).query_type).toBe("range")
    expect(parseExploreState("", emptyMetricCatalog, []).query_type).toBe("range")
  })
})
