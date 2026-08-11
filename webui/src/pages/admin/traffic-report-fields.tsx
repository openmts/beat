import type { AlertChannel, ManagedNode, TrafficReportCadence } from "@/types"
import type { TrafficReportForm } from "@/pages/admin/traffic-report-config"
import { toggleTrafficReportTarget } from "@/pages/admin/traffic-report-config"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

interface TrafficReportFieldsProps {
  form: TrafficReportForm
  nodes: ManagedNode[]
  channels: AlertChannel[]
  t: (key: string) => string
  onChange: (form: TrafficReportForm) => void
}

export function TrafficReportFields(props: TrafficReportFieldsProps) {
  const { form, t, onChange } = props
  const patch = (values: Partial<TrafficReportForm>) => onChange({ ...form, ...values })
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="traffic-report-name">{t("node.name")}</FieldLabel>
        <Input id="traffic-report-name" value={form.name} onChange={(event) => patch({ name: event.target.value })} />
      </Field>
      <CadenceField form={form} t={t} onChange={patch} />
      <TimeFields form={form} t={t} onChange={patch} />
      <NodeScope {...props} onChange={patch} />
      <ChannelScope {...props} onChange={patch} />
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor="traffic-report-enabled">{t("alert.enabled")}</FieldLabel>
        </FieldContent>
        <Switch id="traffic-report-enabled" checked={form.enabled} onCheckedChange={(enabled) => patch({ enabled })} />
      </Field>
    </FieldGroup>
  )
}

function CadenceField(props: Pick<TrafficReportFieldsProps, "form" | "t"> & {
  onChange: (values: Partial<TrafficReportForm>) => void
}) {
  const { form, t, onChange } = props
  return (
    <Field>
      <FieldLabel>{t("traffic_report.cadence")}</FieldLabel>
      <ToggleGroup
        variant="outline"
        spacing={0}
        value={[form.cadence]}
        onValueChange={(values) => values[0] && onChange({ cadence: values[0] as TrafficReportCadence })}
      >
        {(["daily", "weekly", "monthly"] as const).map((cadence) => (
          <ToggleGroupItem key={cadence} value={cadence}>{t(`traffic_report.cadence.${cadence}`)}</ToggleGroupItem>
        ))}
      </ToggleGroup>
    </Field>
  )
}

function TimeFields(props: Pick<TrafficReportFieldsProps, "form" | "t"> & {
  onChange: (values: Partial<TrafficReportForm>) => void
}) {
  const { form, t, onChange } = props
  return (
    <>
      <Field>
        <FieldLabel htmlFor="traffic-report-timezone">{t("traffic_report.timezone")}</FieldLabel>
        <Input id="traffic-report-timezone" value={form.timezone} onChange={(event) => onChange({ timezone: event.target.value })} />
        <FieldDescription>{t("traffic_report.timezone_hint")}</FieldDescription>
      </Field>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <NumberField id="traffic-report-hour" label={t("traffic_report.hour")} value={form.sendHour} min={0} max={23} onChange={(sendHour) => onChange({ sendHour })} />
        <NumberField id="traffic-report-minute" label={t("traffic_report.minute")} value={form.sendMinute} min={0} max={59} onChange={(sendMinute) => onChange({ sendMinute })} />
        {form.cadence === "weekly" ? <WeekdayField form={form} t={t} onChange={onChange} /> : null}
        {form.cadence === "monthly" ? <MonthDayField form={form} t={t} onChange={onChange} /> : null}
      </div>
    </>
  )
}

function WeekdayField(props: Pick<TrafficReportFieldsProps, "form" | "t"> & {
  onChange: (values: Partial<TrafficReportForm>) => void
}) {
  const options = Array.from({ length: 7 }, (_, index) => ({
    value: String(index + 1), label: props.t(`traffic_report.weekday.${index + 1}`),
  }))
  return (
    <Field>
      <FieldLabel htmlFor="traffic-report-weekday">{props.t("traffic_report.weekday")}</FieldLabel>
      <Select items={options} value={props.form.weekday} onValueChange={(weekday) => props.onChange({ weekday: weekday ?? "1" })}>
        <SelectTrigger id="traffic-report-weekday" className="w-full"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>{options.map((option) => (
          <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
        ))}</SelectGroup></SelectContent>
      </Select>
    </Field>
  )
}

function MonthDayField(props: Pick<TrafficReportFieldsProps, "form" | "t"> & {
  onChange: (values: Partial<TrafficReportForm>) => void
}) {
  return (
    <NumberField id="traffic-report-month-day" label={props.t("traffic_report.month_day")} value={props.form.monthDay} min={1} max={31} onChange={(monthDay) => props.onChange({ monthDay })} />
  )
}

function NumberField(props: {
  id: string
  label: string
  value: string
  min: number
  max: number
  onChange: (value: string) => void
}) {
  return (
    <Field>
      <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
      <Input id={props.id} type="number" min={props.min} max={props.max} value={props.value} onChange={(event) => props.onChange(event.target.value)} />
    </Field>
  )
}

function NodeScope(props: TrafficReportFieldsProps & {
  onChange: (values: Partial<TrafficReportForm>) => void
}) {
  const { form, nodes, t, onChange } = props
  return (
    <ScopeField title={t("traffic_report.nodes")} all={form.allNodes} t={t} onAllChange={(allNodes) => onChange({ allNodes })}>
      {nodes.map((node) => (
        <TargetOption key={node.id} id={`traffic-report-node-${node.id}`} label={nodeLabel(node)} checked={form.nodeIds.includes(node.id)} onChange={(checked) => onChange({ nodeIds: toggleTrafficReportTarget(form.nodeIds, node.id, checked) })} />
      ))}
    </ScopeField>
  )
}

function ChannelScope(props: TrafficReportFieldsProps & {
  onChange: (values: Partial<TrafficReportForm>) => void
}) {
  const { form, channels, t, onChange } = props
  return (
    <ScopeField title={t("traffic_report.channels")} all={form.allChannels} t={t} onAllChange={(allChannels) => onChange({ allChannels })}>
      {channels.map((channel) => (
        <TargetOption key={channel.id} id={`traffic-report-channel-${channel.id}`} label={`${channel.name} · ${t(`alert.channel.${channel.channel_type}`)}`} checked={form.channelIds.includes(channel.id)} onChange={(checked) => onChange({ channelIds: toggleTrafficReportTarget(form.channelIds, channel.id, checked) })} />
      ))}
    </ScopeField>
  )
}

function ScopeField(props: {
  title: string
  all: boolean
  t: (key: string) => string
  onAllChange: (all: boolean) => void
  children: React.ReactNode
}) {
  return (
    <FieldSet>
      <FieldLegend variant="label">{props.title}</FieldLegend>
      <ToggleGroup variant="outline" spacing={0} value={[props.all ? "all" : "selected"]} onValueChange={(values) => values[0] && props.onAllChange(values[0] === "all")}>
        <ToggleGroupItem value="all">{props.t("traffic_report.all")}</ToggleGroupItem>
        <ToggleGroupItem value="selected">{props.t("traffic_report.selected")}</ToggleGroupItem>
      </ToggleGroup>
      {!props.all ? <ScrollArea className="h-36 rounded-lg border p-2"><div className="flex flex-col gap-1 pr-2">{props.children}</div></ScrollArea> : null}
    </FieldSet>
  )
}

function TargetOption(props: {
  id: string
  label: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <Field orientation="horizontal" className="rounded-md px-2 py-1.5 hover:bg-muted/50">
      <Checkbox id={props.id} checked={props.checked} onCheckedChange={(checked) => props.onChange(checked === true)} />
      <FieldContent><FieldLabel htmlFor={props.id} className="font-normal">{props.label}</FieldLabel></FieldContent>
    </Field>
  )
}

function nodeLabel(node: ManagedNode): string {
  return node.alias ? `${node.alias} (${node.name})` : node.name
}
