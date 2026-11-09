import { SearchIcon } from "lucide-react"

import { NetworkTargetEditor, newThreeCarrierTargets } from "@/components/network-task-targets"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { networkNodeLabel } from "@/lib/network-quality"
import type {
  NetworkDirection,
  NetworkIPFamily,
  NetworkProbeType,
  NetworkTarget,
  NetworkTargetMode,
  NetworkTaskPayload,
  Node,
} from "@/types"

interface NetworkTaskFieldsProps {
  payload: NetworkTaskPayload
  nodes: Node[]
  search: string
  t: (key: string) => string
  onSearchChange: (value: string) => void
  onChange: (changes: Partial<NetworkTaskPayload>) => void
}

export function NetworkTaskFields(props: NetworkTaskFieldsProps) {
  return (
    <>
      <IdentityFields {...props} />
      <ScheduleFields {...props} />
      <AssignmentFields {...props} />
      <StateFields {...props} />
    </>
  )
}

function IdentityFields({ payload, t, onChange }: NetworkTaskFieldsProps) {
  const options = typeOptions(t)
  const directions = directionOptions(t)
  const changeType = (value: NetworkProbeType) => {
    onChange({
      type: value,
      target: payload.direction === "server_to_agent" ? serverTarget(value) : "",
      packet_count: value === "icmp" ? Math.max(payload.packet_count, 5) : 3,
      targets: payload.targets.map((target) => ({
        ...target, packet_count: value === "icmp" ? Math.max(target.packet_count, 5) : Math.max(target.packet_count, 3),
      })),
    })
  }
  const changeDirection = (value: NetworkDirection) => {
    onChange({
      direction: value,
      target: value === "server_to_agent" ? serverTarget(payload.type) : "",
      target_mode: "single",
      targets: value === "server_to_agent" ? [] : payload.targets,
    })
  }
  const changeTargetMode = (value: NetworkTargetMode) => {
    const targets = value === "group"
      ? payload.targets.length > 0 ? payload.targets : newThreeCarrierTargets(payload.type, t)
      : payload.targets.slice(0, 1)
    onChange({ target_mode: value, targets })
  }
  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="network-task-name">{t("network.name")}</FieldLabel>
          <Input id="network-task-name" value={payload.name} maxLength={100} onChange={(event) => onChange({ name: event.target.value })} />
        </Field>
        <Field>
          <FieldLabel htmlFor="network-task-direction">{t("network.direction")}</FieldLabel>
          <Select items={directions} value={payload.direction} onValueChange={(value) => changeDirection((value ?? "agent_to_target") as NetworkDirection)}>
            <SelectTrigger id="network-task-direction" aria-describedby="network-task-direction-hint" className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent><SelectGroup>{directions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}</SelectGroup></SelectContent>
          </Select>
          <FieldDescription id="network-task-direction-hint">{t(`network.direction_hint.${payload.direction}`)}</FieldDescription>
        </Field>
      </div>
      {payload.direction === "agent_to_target" && (
        <Field>
          <FieldLabel>{t("network.target_mode")}</FieldLabel>
          <ToggleGroup
            variant="outline"
            spacing={0}
            value={[payload.target_mode]}
            onValueChange={(value) => value[0] && changeTargetMode(value[0] as NetworkTargetMode)}
          >
            <ToggleGroupItem value="single" aria-label={t("network.target_mode.single")}>{t("network.target_mode.single")}</ToggleGroupItem>
            <ToggleGroupItem value="group" aria-label={t("network.target_mode.group")}>{t("network.target_mode.group")}</ToggleGroupItem>
          </ToggleGroup>
          <FieldDescription>{t(`network.target_mode_hint.${payload.target_mode}`)}</FieldDescription>
        </Field>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="network-task-type">{t("app.type")}</FieldLabel>
          <Select items={options} value={payload.type} onValueChange={(value) => changeType((value ?? "icmp") as NetworkProbeType)}>
            <SelectTrigger id="network-task-type" className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent><SelectGroup>{options.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}</SelectGroup></SelectContent>
          </Select>
        </Field>
        {payload.direction === "agent_to_target" && payload.target_mode === "group" ? null : (
          <Field>
            <FieldLabel htmlFor="network-task-target">{t("network.target")}</FieldLabel>
            <Input
              id="network-task-target"
              value={payload.target}
              onChange={(event) => onChange({ target: event.target.value })}
              placeholder={payload.direction === "server_to_agent"
                ? t("network.target_hint.server")
                : t(`network.target_hint.${payload.type}`)}
            />
          </Field>
        )}
      </div>
      {payload.direction === "agent_to_target" && payload.target_mode === "group" && (
        <NetworkTargetEditor
          targets={payload.targets}
          type={payload.type}
          t={t}
          onChange={(targets: NetworkTarget[]) => onChange({ targets })}
        />
      )}
    </>
  )
}

function ScheduleFields({ payload, t, onChange }: NetworkTaskFieldsProps) {
  const options = familyOptions(t)
  const grouped = payload.direction === "agent_to_target" && payload.target_mode === "group"
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {!grouped && <Field>
        <FieldLabel htmlFor="network-ip-family">{t("network.ip_family")}</FieldLabel>
        <Select items={options} value={payload.ip_family} onValueChange={(value) => onChange({ ip_family: (value ?? "auto") as NetworkIPFamily })}>
          <SelectTrigger id="network-ip-family" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent><SelectGroup>{options.map((option) => (
            <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
          ))}</SelectGroup></SelectContent>
        </Select>
      </Field>}
      <NumberField id="network-interval" label={t("network.interval_seconds")} value={payload.interval_seconds} min={10} max={86400} onChange={(value) => onChange({ interval_seconds: value })} />
      <NumberField id="network-timeout" label={t("network.timeout_ms")} value={payload.timeout_milliseconds} min={100} max={30000} onChange={(value) => onChange({ timeout_milliseconds: value })} />
      {!grouped && (payload.type === "icmp" || payload.direction === "agent_to_target") && (
        <NumberField
          id="network-packet-count"
          label={t(payload.type === "icmp" ? "network.packet_count" : "network.attempt_count")}
          value={payload.packet_count}
          min={1}
          max={20}
          onChange={(value) => onChange({ packet_count: value })}
        />
      )}
    </div>
  )
}

function NumberField(props: { id: string; label: string; value: number; min: number; max: number; onChange: (value: number) => void }) {
  return (
    <Field>
      <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
      <Input id={props.id} type="number" min={props.min} max={props.max} value={props.value} onChange={(event) => props.onChange(Number(event.target.value))} />
    </Field>
  )
}

function AssignmentFields(props: NetworkTaskFieldsProps) {
  const { payload, t, onChange } = props
  return (
    <>
      <Field>
        <FieldLabel>{t("network.assignment")}</FieldLabel>
        <ToggleGroup
          variant="outline"
          spacing={0}
          value={[payload.all_nodes ? "all" : "selected"]}
          onValueChange={(value) => value[0] && onChange({ all_nodes: value[0] === "all" })}
        >
          <ToggleGroupItem value="all" aria-label={t("network.all_nodes")}>{t("network.all_nodes")}</ToggleGroupItem>
          <ToggleGroupItem value="selected" aria-label={t("network.selected_nodes")}>{t("network.selected_nodes")}</ToggleGroupItem>
        </ToggleGroup>
      </Field>
      {!payload.all_nodes && <NodeChecklist {...props} />}
    </>
  )
}

function NodeChecklist({ payload, nodes, search, t, onSearchChange, onChange }: NetworkTaskFieldsProps) {
  const query = search.trim().toLocaleLowerCase()
  const filtered = query ? nodes.filter((node) => networkNodeLabel(node).toLocaleLowerCase().includes(query)) : nodes
  return (
    <FieldSet>
      <FieldLegend variant="label">{t("network.nodes")}</FieldLegend>
      <FieldDescription>{t("network.nodes_hint")}</FieldDescription>
      <div className="relative">
        <SearchIcon className="pointer-events-none absolute top-2 left-2.5 size-4 text-muted-foreground" />
        <Input className="pl-8" value={search} onChange={(event) => onSearchChange(event.target.value)} placeholder={t("app.search")} />
      </div>
      <ScrollArea className="h-40 rounded-lg border p-2">
        <div data-slot="checkbox-group" className="flex flex-col gap-1 pr-2">
          {filtered.map((node) => <NodeOption key={node.id} node={node} checked={payload.node_ids.includes(node.id)} onChange={(checked) => onChange({ node_ids: toggleNode(payload.node_ids, node.id, checked) })} />)}
        </div>
      </ScrollArea>
    </FieldSet>
  )
}

function NodeOption({ node, checked, onChange }: { node: Node; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <Field orientation="horizontal" className="rounded-md px-2 py-1.5 hover:bg-muted/50">
      <Checkbox id={`network-node-${node.id}`} checked={checked} onCheckedChange={(value) => onChange(value === true)} />
      <FieldContent><FieldLabel htmlFor={`network-node-${node.id}`} className="font-normal">{networkNodeLabel(node)}</FieldLabel></FieldContent>
    </Field>
  )
}

function StateFields({ payload, t, onChange }: NetworkTaskFieldsProps) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <BooleanField id="network-enabled" label={t("alert.enabled")} checked={payload.enabled} onChange={(enabled) => onChange({ enabled })} />
      <BooleanField id="network-public" label={t("network.public")} checked={payload.is_public} onChange={(is_public) => onChange({ is_public })} />
    </div>
  )
}

function BooleanField({ id, label, checked, onChange }: { id: string; label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <Field orientation="horizontal">
      <FieldContent><FieldLabel htmlFor={id}>{label}</FieldLabel></FieldContent>
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
    </Field>
  )
}

function toggleNode(ids: string[], id: string, checked: boolean): string[] {
  return checked ? [...new Set([...ids, id])] : ids.filter((current) => current !== id)
}

function typeOptions(t: (key: string) => string) {
  return (["icmp", "tcp", "http"] as const).map((value) => ({ label: t(`network.type.${value}`), value }))
}

function familyOptions(t: (key: string) => string) {
  return (["auto", "ipv4", "ipv6"] as const).map((value) => ({ label: t(`network.family.${value}`), value }))
}

function directionOptions(t: (key: string) => string) {
  return (["agent_to_target", "server_to_agent"] as const).map((value) => ({ label: t(`network.direction.${value}`), value }))
}

function serverTarget(type: NetworkProbeType): string {
  if (type === "tcp") return "{{host_port}}"
  if (type === "http") return "http://{{host}}/"
  return "{{host}}"
}
