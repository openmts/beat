import { Field, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  type SelectOption,
} from "@/components/ui/select"

interface AdminSelectFieldProps {
  id?: string
  label: string
  options: ReadonlyArray<SelectOption>
  value: string
  onValueChange: (value: string) => void
  disabled?: boolean
}

export function AdminSelectField({
  id,
  label,
  options,
  value,
  onValueChange,
  disabled = false,
}: AdminSelectFieldProps) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select
        items={options}
        value={value}
        disabled={disabled}
        onValueChange={(nextValue) => { if (nextValue) onValueChange(nextValue) }}
      >
        <SelectTrigger id={id}><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </Field>
  )
}
