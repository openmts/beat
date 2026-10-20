import { useState } from "react"
import { SearchIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useLocale } from "@/context/locale"
import { workspaceAllSelection, workspaceVariableOptions, type WorkspaceVariableOption } from "@/lib/workspace-variable-options"
import { WorkspaceSelect } from "./select"
import type { Group, Node } from "@/types"
import type { DashboardVariable, WorkspaceSelections } from "@/types/metric-workspace"

export function WorkspaceVariables(props: {
  variables: DashboardVariable[]; values: WorkspaceSelections; nodes: Node[]; groups: Group[]
  onChange: (name: string, values: string[]) => void
}) {
  return <div className="flex flex-wrap items-end gap-3">{props.variables.map((variable) => <WorkspaceVariable key={variable.name} variable={variable}
    selected={props.values[variable.name] ?? []} options={workspaceVariableOptions(variable, props.nodes, props.groups)}
    onChange={(values) => props.onChange(variable.name, values)} />)}</div>
}

function WorkspaceVariable(props: {
  variable: DashboardVariable; selected: string[]; options: WorkspaceVariableOption[]; onChange: (values: string[]) => void
}) {
  return <div className="grid min-w-36 gap-1.5">
    <span className="text-xs font-medium text-muted-foreground">{props.variable.label}</span>
    {props.variable.multi
      ? <WorkspaceMultiSelect variable={props.variable} selected={props.selected} options={props.options} onChange={props.onChange} />
      : <WorkspaceSelect label={props.variable.label} value={props.selected[0] ?? ""} options={props.options}
        onChange={(value) => props.onChange([value])} className="w-full max-w-64" />}
  </div>
}

/** 多选变量用对话框挑选：长节点列表需要搜索，复选框列表沿用网络任务的选择方式。 */
function WorkspaceMultiSelect(props: {
  variable: DashboardVariable; selected: string[]; options: WorkspaceVariableOption[]; onChange: (values: string[]) => void
}) {
  const { t } = useLocale()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const query = search.trim().toLocaleLowerCase()
  const filtered = query ? props.options.filter((option) => option.label.toLocaleLowerCase().includes(query)) : props.options
  const allSelected = props.selected.includes(workspaceAllSelection)
  function toggle(value: string, checked: boolean) {
    if (value === workspaceAllSelection) { props.onChange(checked ? [workspaceAllSelection] : []); return }
    props.onChange(checked ? [...props.selected, value] : props.selected.filter((candidate) => candidate !== value))
  }
  return <>
    <Button type="button" variant="outline" size="sm" aria-label={props.variable.label} className="w-full max-w-64 justify-start font-normal" onClick={() => setOpen(true)}>
      <span className="truncate">{multiSelectionLabel(props.selected, props.options, t)}</span>
    </Button>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="sm:max-w-md">
      <DialogHeader><DialogTitle>{props.variable.label}</DialogTitle>
        <DialogDescription>{t("workspace.variable_picker_hint")}</DialogDescription></DialogHeader>
      <div className="relative"><SearchIcon className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
        <Input className="pl-8" value={search} onChange={(event) => setSearch(event.target.value)} placeholder={t("app.search")} /></div>
      <ScrollArea className="h-56 rounded-lg border p-2">
        <div data-slot="checkbox-group" className="flex flex-col gap-1 pr-2">
          {props.variable.include_all === true && <Field orientation="horizontal" className="rounded-md border-b px-2 py-1.5">
            <Checkbox id={`workspace-variable-${props.variable.name}-all`} checked={allSelected}
              onCheckedChange={(checked) => toggle(workspaceAllSelection, checked === true)} />
            <FieldContent><FieldLabel htmlFor={`workspace-variable-${props.variable.name}-all`} className="font-normal">{t("workspace.all")}</FieldLabel></FieldContent>
          </Field>}
          {filtered.map((option) => <Field key={option.value} orientation="horizontal" className="rounded-md px-2 py-1.5 hover:bg-muted/50">
            <Checkbox id={`workspace-variable-${props.variable.name}-${option.value}`} checked={allSelected || props.selected.includes(option.value)} disabled={allSelected}
              onCheckedChange={(checked) => toggle(option.value, checked === true)} />
            <FieldContent><FieldLabel htmlFor={`workspace-variable-${props.variable.name}-${option.value}`} className="font-normal">{option.label}</FieldLabel></FieldContent>
          </Field>)}
          {!filtered.length && <p className="px-2 py-1.5 text-sm text-muted-foreground">{t("workspace.no_data")}</p>}
        </div>
      </ScrollArea>
      <DialogFooter><Button onClick={() => setOpen(false)}>{t("workspace.done")}</Button></DialogFooter>
    </DialogContent></Dialog>
  </>
}

/** 摘要显示前两个已选项并统计其余项；没有选择时给出变量标签，按钮不会变成空框。 */
function multiSelectionLabel(selected: string[], options: WorkspaceVariableOption[], t: (key: string, values?: Record<string, string | number>) => string) {
  if (selected.includes(workspaceAllSelection)) return t("workspace.all")
  const labels = options.filter((option) => selected.includes(option.value)).map((option) => option.label)
  if (!labels.length) return t("workspace.select_node")
  const remaining = labels.length - 2
  return remaining > 0 ? `${labels.slice(0, 2).join(", ")} ${t("workspace.variable_selected", { count: remaining })}` : labels.join(", ")
}
