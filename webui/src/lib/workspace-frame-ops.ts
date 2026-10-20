// 结果帧的列级操作：filter_by_value 的按列过滤与 sort_by 的按列排序都从这里取列。
// 只以类型方式引用帧模型，避免与变换模块形成运行时循环依赖。
import type { WorkspaceFrameRow, WorkspaceTableFrame } from "@/lib/workspace-transformations"

/** 列选择器与服务端 MaxTransformationColumnBytes 同上限：列名只是结果帧里的一个表头。 */
export const maximumFrameColumnBytes = 64

/**
 * frameColumnIndex 按列键或数据自带的列名定位一列。列键（`field`、`calculation_max`、`group_node_id`）
 * 与语言无关，数据列名只在分组键这类场景下出现；找不到时返回 -1，调用方按「这一步不生效」处理。
 */
export function frameColumnIndex(frame: WorkspaceTableFrame, name: string): number {
  return frame.columns.findIndex((column) => column.key === name || column.label === name)
}

/**
 * frameCalculationColumn 找出某个计算方式对应的数值列：先按列键 `calculation_<计算方式>` 匹配，再按
 * 本地化的计算方式后缀匹配（历史帧里列键与计算方式曾同名），最后在帧恰有一个数值列时退化用它。
 */
export function frameCalculationColumn(frame: WorkspaceTableFrame, statistic: string): number {
  const calculationKey = `workspace.calculation_${statistic}`
  const named = frame.columns.findIndex((column) => column.value === true &&
    (column.key === `calculation_${statistic}` || column.calculationKey === calculationKey))
  if (named >= 0) return named
  const values = frame.columns.flatMap((column, position) => (column.value === true ? [position] : []))
  return values.length === 1 ? values[0]! : -1
}

/** filterFrameRows 只裁剪行，列的值由调用方判定；判定为真的行保留，与表格面板的过滤口径一致。 */
export function filterFrameRows(frame: WorkspaceTableFrame, column: number, keep: (value: number | null) => boolean): WorkspaceTableFrame {
  const rows: WorkspaceFrameRow[] = frame.rows.filter((row) => keep(row.cells[column]?.value ?? null))
  return { ...frame, rows }
}
