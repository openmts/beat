import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

export function WorkspaceSelect(props: {
  label: string; value: string; options: { value: string; label: string }[]
  onChange: (value: string) => void; disabled?: boolean; className?: string
}) {
  return <Select value={props.value} items={props.options} disabled={props.disabled} onValueChange={(value) => { if (value !== null) props.onChange(value) }}>
    <SelectTrigger aria-label={props.label} className={props.className}><SelectValue placeholder={props.label} /></SelectTrigger>
    <SelectContent>{props.options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
  </Select>
}
