import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { emptyValueMapping, maximumMappingTextBytes, maximumValueMappings, switchValueMappingType, valueMappingError, valueMappingTypes } from "@/lib/workspace-value-mapping"
import { emptyTransformation, maximumPanelTransformations, switchTransformationKind, transformationComparisons, transformationError, transformationGroupCalculations, transformationGroupFields, transformationKinds, transformationOperators, transformationReduceModes, transformationStatistics } from "@/lib/workspace-transformations"
import { panelCalculations } from "@/lib/workspace-calculation"
import { Checkbox } from "@/components/ui/checkbox"
import { WorkspaceSelect } from "./select"
import { useLegendText } from "./legend-text"
import type { DashboardPanel, DashboardTransformation, DashboardValueMapping, LegendValue, PanelCalculation, PanelColor, TransformationComparison, TransformationGroupCalculation, TransformationGroupField, TransformationOperator, TransformationOrder, TransformationReduceMode, ValueMappingType } from "@/types/metric-workspace"

/** 比较符号本身无语言差异，直接展示符号，避免为六个运算符各写五份文案。 */
const comparisonLabels: Record<TransformationComparison, string> = { gt: ">", gte: "≥", lt: "<", lte: "≤", eq: "=", ne: "≠" }

/** Grafana 的值映射：精确值、区间或空样本覆盖为显示文本与颜色；只对 stat/gauge/table 显示。 */
export function WorkspaceValueMappings(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const mappings = panel.value_mappings ?? []
  const error = valueMappingError(panel)
  const change = (index: number, patch: Partial<DashboardValueMapping>) =>
    update({ ...panel, value_mappings: mappings.map((mapping, current) => current === index ? { ...mapping, ...patch } : mapping) })
  const numbers = (mapping: DashboardValueMapping, index: number) => mapping.type === "range"
    ? ([["from", "workspace.value_mapping_from"], ["to", "workspace.value_mapping_to"]] as const).map(([key, label]) =>
      <Input key={key} className="max-w-24" type="number" aria-label={`${t(label)} ${index + 1}`} placeholder={key === "from" ? "-∞" : "+∞"}
        value={mapping[key] ?? ""} onChange={(event) => change(index, { [key]: event.target.value === "" ? undefined : Number(event.target.value) })} />)
    : mapping.type === "special"
      ? <span key="match" className="text-xs text-muted-foreground">{t("workspace.value_mapping_null")}</span>
      : <Input key="value" className="max-w-24" type="number" aria-label={`${t("workspace.value_mapping_value")} ${index + 1}`}
          value={mapping.value ?? ""} onChange={(event) => change(index, { value: event.target.value === "" ? undefined : Number(event.target.value) })} />
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.value_mappings")}</legend>
    <p className="text-xs text-muted-foreground">{t("workspace.value_mappings_hint")}</p>
    {mappings.map((mapping, index) => <div key={index} className="flex flex-wrap items-center gap-3 rounded-md border p-2">
      <WorkspaceSelect label={`${t("workspace.value_mapping_type")} ${index + 1}`} value={mapping.type} options={valueMappingTypes.map((kind) => ({ value: kind, label: t(`workspace.value_mapping_type_${kind}`) }))}
        onChange={(kind) => change(index, switchValueMappingType(mapping, kind as ValueMappingType))} />
      {numbers(mapping, index)}
      <Input className="max-w-40" maxLength={maximumMappingTextBytes} aria-label={`${t("workspace.value_mapping_text")} ${index + 1}`} placeholder={t("workspace.value_mapping_text")}
        value={mapping.text ?? ""} onChange={(event) => change(index, { text: event.target.value || undefined })} />
      <WorkspaceSelect label={`${t("workspace.color")} ${index + 1}`} value={mapping.color ?? ""} options={[{ value: "", label: t("workspace.color_inherit") }, ...Object.keys(panelColors).map((color) => ({ value: color, label: t(`workspace.${color}`) }))]}
        onChange={(color) => change(index, { color: color ? color as PanelColor : undefined })} />
      <Button size="icon-sm" variant="ghost" aria-label={`${t("workspace.value_mapping_remove")} ${index + 1}`} onClick={() => update({ ...panel, value_mappings: mappings.filter((_, current) => current !== index) })}><XIcon /></Button>
    </div>)}
    <Button variant="outline" size="sm" disabled={mappings.length >= maximumValueMappings} onClick={() => update({ ...panel, value_mappings: [...mappings, emptyValueMapping()] })}>{t("workspace.add_value_mapping")}</Button>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Transform 标签页：查询结果按顺序过一遍变换链再渲染；空链表示原样渲染。 */
export function WorkspaceTransformations(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const text = useLegendText()
  const panel = props.panel, update = props.update
  const chain = panel.transformations ?? []
  const error = transformationError(panel)
  const change = (index: number, transformation: DashboardTransformation) =>
    update({ ...panel, transformations: chain.map((value, current) => current === index ? transformation : value) })
  const fields = (transformation: DashboardTransformation, index: number) => {
    if (transformation.kind === "rename_by_regex") return <>
      <Input className="max-w-56" aria-label={`${t("workspace.transformation_pattern")} ${index + 1}`} placeholder={t("workspace.transformation_pattern")}
        value={transformation.pattern ?? ""} onChange={(event) => change(index, { ...transformation, pattern: event.target.value || undefined })} />
      <Input className="max-w-56" aria-label={`${t("workspace.transformation_replacement")} ${index + 1}`} placeholder={t("workspace.transformation_replacement")}
        value={transformation.replacement ?? ""} onChange={(event) => change(index, { ...transformation, replacement: event.target.value })} />
    </>
    if (transformation.kind === "filter_by_value") return <>
      <WorkspaceSelect label={`${t("workspace.transformation_comparison")} ${index + 1}`} value={transformation.comparison ?? "gt"}
        options={transformationComparisons.map((comparison) => ({ value: comparison, label: comparisonLabels[comparison] }))}
        onChange={(comparison) => change(index, { ...transformation, comparison: comparison as TransformationComparison })} />
      <Input className="max-w-32" type="number" aria-label={`${t("workspace.transformation_value")} ${index + 1}`} placeholder={t("workspace.transformation_value")}
        value={transformation.value ?? ""} onChange={(event) => change(index, { ...transformation, value: event.target.value === "" ? undefined : Number(event.target.value) })} />
      {/* 可选列选择：填了它这一步就按结果帧的这一列过滤行，留空仍旧按序列取值过滤。 */}
      <Input className="max-w-40" aria-label={`${t("workspace.transformation_column")} ${index + 1}`} placeholder={t("workspace.transformation_column")}
        value={transformation.column ?? ""} onChange={(event) => change(index, { ...transformation, column: event.target.value || undefined })} />
    </>
    if (transformation.kind === "filter_by_label") return <>
      <Input className="max-w-56" aria-label={`${t("workspace.transformation_pattern")} ${index + 1}`} placeholder={t("workspace.transformation_pattern")}
        value={transformation.pattern ?? ""} onChange={(event) => change(index, { ...transformation, pattern: event.target.value || undefined })} />
      <label className="flex items-center gap-2 text-sm"><Checkbox checked={transformation.invert === true}
        onCheckedChange={(checked) => change(index, { ...transformation, invert: checked === true ? true : undefined })} />{t("workspace.transformation_invert")}</label>
    </>
    if (transformation.kind === "binary_operation") return <>
      <Input className="max-w-40" aria-label={`${t("workspace.transformation_left")} ${index + 1}`} placeholder={t("workspace.transformation_left")}
        value={transformation.left ?? ""} onChange={(event) => change(index, { ...transformation, left: event.target.value || undefined })} />
      <WorkspaceSelect label={`${t("workspace.transformation_operator")} ${index + 1}`} value={transformation.operator ?? "+"}
        options={transformationOperators.map((operator) => ({ value: operator, label: operator }))}
        onChange={(operator) => change(index, { ...transformation, operator: operator as TransformationOperator })} />
      <Input className="max-w-40" aria-label={`${t("workspace.transformation_right")} ${index + 1}`} placeholder={t("workspace.transformation_right")}
        value={transformation.right ?? ""} onChange={(event) => change(index, { ...transformation, right: event.target.value || undefined })} />
      <Input className="max-w-40" aria-label={`${t("workspace.transformation_alias")} ${index + 1}`} placeholder={t("workspace.transformation_alias")}
        value={transformation.alias ?? ""} onChange={(event) => change(index, { ...transformation, alias: event.target.value || undefined })} />
    </>
    if (transformation.kind === "reduce") {
      // 历史看板写单个 calculation；编辑器统一按 calculations 渲染，保存时两种写法只留一种。
      const calculations = transformation.calculations ?? (transformation.calculation ? [transformation.calculation] : [])
      const mode = transformation.mode ?? "reduce_fields"
      const setMode = (next: TransformationReduceMode) => change(index, { ...transformation, mode: next, calculation: undefined,
        calculations: next === "reduce_fields" ? calculations.slice(0, 1) : (calculations.length ? calculations : ["mean"]) })
      const toggle = (calculation: PanelCalculation, checked: boolean) => {
        const next = checked ? [...calculations, calculation] : calculations.filter((value) => value !== calculation)
        change(index, { ...transformation, mode: "series_to_rows", calculation: undefined, calculations: next })
      }
      return <div className="flex flex-wrap items-center gap-3">
        <WorkspaceSelect label={`${t("workspace.transformation_reduce_mode")} ${index + 1}`} value={mode}
          options={transformationReduceModes.map((value) => ({ value, label: t(`workspace.transformation_reduce_mode_${value}`) }))}
          onChange={(value) => setMode(value as TransformationReduceMode)} />
        {mode === "series_to_rows" ? <div className="flex flex-wrap items-center gap-3" role="group"
          aria-label={`${t("workspace.transformation_calculations")} ${index + 1}`}>
          {panelCalculations.map((calculation) => <label key={calculation} className="flex items-center gap-1 text-sm">
            <Checkbox checked={calculations.includes(calculation)} onCheckedChange={(checked) => toggle(calculation, checked === true)} />
            {t(`workspace.calculation_${calculation}`)}</label>)}
        </div> : <WorkspaceSelect label={`${t("workspace.transformation_calculation")} ${index + 1}`} value={calculations[0] ?? "last"}
          options={panelCalculations.map((calculation) => ({ value: calculation, label: t(`workspace.calculation_${calculation}`) }))}
          onChange={(calculation) => change(index, { ...transformation, calculation: undefined, calculations: [calculation as PanelCalculation] })} />}
      </div>
    }
    if (transformation.kind === "group_by") {
      // 与 Grafana 一样可以选多个计算方式：每个计算方式各生成一条合并序列，标签可另起名字。
      const calculations = transformation.calculations ?? (transformation.calculation ? [transformation.calculation] : [])
      const toggle = (calculation: TransformationGroupCalculation, checked: boolean) => {
        const next = checked ? [...calculations, calculation] : calculations.filter((value) => value !== calculation)
        // 至少留一个计算方式，避免保存出渲染器没有结果可用的空分组。
        if (next.length === 0) return
        change(index, { ...transformation, calculation: undefined, calculations: next })
      }
      return <>
        <WorkspaceSelect label={`${t("workspace.transformation_group_by")} ${index + 1}`} value={transformation.group_by ?? "metric"}
          options={transformationGroupFields.map((field) => ({ value: field, label: t(`workspace.transformation_group_field_${field}`) }))}
          onChange={(field) => change(index, { ...transformation, group_by: field as TransformationGroupField })} />
        <Input className="max-w-40" aria-label={`${t("workspace.transformation_alias")} ${index + 1}`} placeholder={t("workspace.transformation_alias")}
          value={transformation.alias ?? ""} onChange={(event) => change(index, { ...transformation, alias: event.target.value || undefined })} />
        <div className="flex flex-wrap items-center gap-3" role="group" aria-label={`${t("workspace.transformation_calculations")} ${index + 1}`}>
          {transformationGroupCalculations.map((calculation) => <label key={calculation} className="flex items-center gap-1 text-sm">
            <Checkbox checked={calculations.includes(calculation)} onCheckedChange={(checked) => toggle(calculation, checked === true)} />
            {t(`workspace.calculation_${calculation}`)}</label>)}
        </div>
      </>
    }
    if (transformation.kind === "sort_by") return <>
      <WorkspaceSelect label={`${t("workspace.transformation_statistic")} ${index + 1}`} value={transformation.statistic ?? "last"}
        options={transformationStatistics.map((statistic) => ({ value: statistic, label: text(statistic) }))}
        onChange={(statistic) => change(index, { ...transformation, statistic: statistic as LegendValue })} />
      {/* 可选字段：填了它排序直接按结果帧的这一列（可以是分组键列），留空沿用统计量的同名计算列。 */}
      <Input className="max-w-40" aria-label={`${t("workspace.transformation_field")} ${index + 1}`} placeholder={t("workspace.transformation_field")}
        value={transformation.field ?? ""} onChange={(event) => change(index, { ...transformation, field: event.target.value || undefined })} />
      <WorkspaceSelect label={`${t("workspace.transformation_order")} ${index + 1}`} value={transformation.order ?? "desc"}
        options={[{ value: "asc", label: text("sort_asc") }, { value: "desc", label: text("sort_desc") }]}
        onChange={(order) => change(index, { ...transformation, order: order as TransformationOrder })} />
    </>
    return <Input className="max-w-32" type="number" aria-label={`${t("workspace.transformation_count")} ${index + 1}`}
      value={transformation.count ?? ""} onChange={(event) => change(index, { ...transformation, count: event.target.value === "" ? undefined : Number(event.target.value) })} />
  }
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.transformations")}</legend>
    <p className="text-xs text-muted-foreground">{t("workspace.transformation_hint")}</p>
    {chain.map((transformation, index) => <div key={index} className="flex flex-wrap items-center gap-3 rounded-md border p-2">
      <WorkspaceSelect label={`${t("workspace.transformation_kind")} ${index + 1}`} value={transformation.kind}
        options={transformationKinds.map((kind) => ({ value: kind, label: t(`workspace.transformation_kind_${kind}`) }))}
        onChange={(kind) => change(index, switchTransformationKind(kind))} />
      {fields(transformation, index)}
      <Button size="icon-sm" variant="ghost" aria-label={`${t("workspace.transformation_remove")} ${index + 1}`}
        onClick={() => update({ ...panel, transformations: chain.filter((_, current) => current !== index) })}><XIcon /></Button>
    </div>)}
    <Button variant="outline" size="sm" disabled={chain.length >= maximumPanelTransformations}
      onClick={() => update({ ...panel, transformations: [...chain, emptyTransformation()] })}>{t("workspace.add_transformation")}</Button>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}
