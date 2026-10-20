/** 百分比堆叠的行数据：保留时间点与各序列的归一化取值，null 仍是缺失样本。 */
export type WorkspaceStackRow = Record<string, number | null>

/** 每个时间点的绝对值总量，用于把百分比坐标还原成 tooltip 里的原始值。 */
export type StackTotals = Map<number, number>

/**
 * percentStackRows 实现 Grafana 的百分比堆叠：按时间点把所有序列的绝对值归一化成 100%，
 * 同时给出还原用的总量表。总量为 0 的时间点保持原值，避免除零把空样本变成 0。
 */
export function percentStackRows(rows: WorkspaceStackRow[], refIds: string[]): { rows: WorkspaceStackRow[]; totals: StackTotals } {
  const totals: StackTotals = new Map()
  const stacked = rows.map((row) => {
    const timestamp = Number(row.timestamp)
    const total = refIds.reduce((sum, refId) => sum + Math.abs(Number(row[refId] ?? 0)), 0)
    totals.set(timestamp, total)
    const next: WorkspaceStackRow = Object.assign(Object.create(null), { timestamp: row.timestamp })
    for (const refId of refIds) {
      const value = row[refId]
      next[refId] = value === null || value === undefined || total === 0 ? value ?? null : Number(value) / total * 100
    }
    return next
  })
  return { rows: stacked, totals }
}

/**
 * percentStackGroups 按轴分组做百分比堆叠：每组各自归一化成 100%，返回合并后的行数据与逐序列总量表。
 * 单轴面板只有一组，结果与直接调用 percentStackRows 完全一致；双 Y 轴面板按轴分别取满刻度。
 */
export function percentStackGroups(rows: WorkspaceStackRow[], groups: string[][]): { rows: WorkspaceStackRow[]; totals: Map<string, StackTotals> } {
  const totals = new Map<string, StackTotals>()
  const merged = rows.map((row) => Object.assign(Object.create(null), row))
  for (const group of groups) {
    if (group.length === 0) continue
    const stacked = percentStackRows(rows, group)
    for (const [index, row] of stacked.rows.entries()) for (const refId of group) merged[index][refId] = row[refId]
    for (const refId of group) totals.set(refId, stacked.totals)
  }
  return { rows: merged, totals }
}

/** stackedTooltipValueByRef 在双 Y 轴百分比堆叠下按序列取各自的总量表；查不到时按坐标原样返回。 */
export function stackedTooltipValueByRef(value: number, item: unknown, totals: Map<string, StackTotals> | null): number {
  if (totals === null) return value
  const refId = String((item as { dataKey?: unknown } | undefined)?.dataKey ?? "")
  const group = totals.get(refId)
  return group === undefined ? value : stackedTooltipValue(value, item, group)
}

/** percentStackValue 把百分比堆叠坐标还原成原始值；总量为 0 或时间点缺失时按坐标原样返回。 */
export function percentStackValue(value: number, timestamp: number, totals: StackTotals): number {
  const total = totals.get(timestamp) ?? 0
  return total > 0 ? value * total / 100 : value
}

/** stackedTooltipValue 供 tooltip 使用：非百分比堆叠（totals 为 null）时原样返回坐标值。 */
export function stackedTooltipValue(value: number, item: unknown, totals: StackTotals | null): number {
  if (totals === null) return value
  const timestamp = Number((item as { payload?: { timestamp?: number } } | undefined)?.payload?.timestamp ?? Number.NaN)
  return percentStackValue(value, timestamp, totals)
}
