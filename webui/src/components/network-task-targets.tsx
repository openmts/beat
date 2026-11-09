import { PlusIcon, Trash2Icon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Field, FieldContent, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import type { NetworkCarrier, NetworkIPFamily, NetworkProbeType, NetworkTarget } from "@/types"

const carriers: NetworkCarrier[] = ["mobile", "unicom", "telecom", "custom"]

export function NetworkTargetEditor(props: {
  targets: NetworkTarget[]
  type: NetworkProbeType
  t: (key: string) => string
  onChange: (targets: NetworkTarget[]) => void
}) {
  const { targets, type, t, onChange } = props
  const update = (index: number, changes: Partial<NetworkTarget>) => {
    onChange(targets.map((target, current) => current === index ? { ...target, ...changes } : target))
  }
  const addTarget = (carrier: NetworkCarrier) => {
    onChange([...targets, newTarget(carrier, type, targets.length, t)])
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium">{t("network.targets")}</p>
          <p className="text-xs text-muted-foreground">{t("network.targets_hint")}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => onChange(newThreeCarrierTargets(type, t))}>
            <PlusIcon />{t("network.add_three_carriers")}
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => addTarget("custom")}>
            <PlusIcon />{t("network.add_custom_target")}
          </Button>
        </div>
      </div>
      {targets.length === 0 ? (
        <div className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
          {t("network.targets_empty")}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {targets.map((target, index) => (
            <Card key={`${target.id || "new"}-${index}`} size="sm">
              <CardContent className="grid gap-3 p-3 sm:grid-cols-2 lg:grid-cols-4">
                <Field>
                  <FieldLabel htmlFor={`target-label-${index}`}>{t("network.target_label")}</FieldLabel>
                  <Input id={`target-label-${index}`} value={target.label} maxLength={100}
                    onChange={(event) => update(index, { label: event.target.value })} />
                </Field>
                <Field>
                  <FieldLabel htmlFor={`target-carrier-${index}`}>{t("network.carrier")}</FieldLabel>
                  <Select items={carrierOptions(t)} value={target.carrier}
                    onValueChange={(value) => update(index, { carrier: (value ?? "custom") as NetworkCarrier })}>
                    <SelectTrigger id={`target-carrier-${index}`} className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent><SelectGroup>{carrierOptions(t).map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}</SelectGroup></SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel htmlFor={`target-region-${index}`}>{t("network.region")}</FieldLabel>
                  <Input id={`target-region-${index}`} value={target.region} maxLength={100}
                    onChange={(event) => update(index, { region: event.target.value })} />
                </Field>
                <Field>
                  <FieldLabel htmlFor={`target-address-${index}`}>{t("network.target")}</FieldLabel>
                  <Input id={`target-address-${index}`} value={target.target} maxLength={2048}
                    placeholder={t(`network.target_hint.${type}`)}
                    onChange={(event) => update(index, { target: event.target.value })} />
                </Field>
                <Field>
                  <FieldLabel htmlFor={`target-family-${index}`}>{t("network.ip_family")}</FieldLabel>
                  <Select items={familyOptions(t)} value={target.ip_family}
                    onValueChange={(value) => update(index, { ip_family: (value ?? "auto") as NetworkIPFamily })}>
                    <SelectTrigger id={`target-family-${index}`} className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent><SelectGroup>{familyOptions(t).map((option) => (
                      <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                    ))}</SelectGroup></SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel htmlFor={`target-samples-${index}`}>{sampleLabel(type, t)}</FieldLabel>
                  <Input id={`target-samples-${index}`} type="number" min={1} max={20} value={target.packet_count}
                    onChange={(event) => update(index, { packet_count: Number(event.target.value) })} />
                </Field>
                <Field orientation="horizontal">
                  <FieldContent><FieldLabel htmlFor={`target-visible-${index}`}>{t("network.visible_address")}</FieldLabel></FieldContent>
                  <Switch id={`target-visible-${index}`} checked={target.visible_address}
                    onCheckedChange={(visible_address) => update(index, { visible_address })} />
                </Field>
                <div className="flex items-end justify-between gap-2">
                  <Field orientation="horizontal" className="min-w-0">
                    <FieldContent><FieldLabel htmlFor={`target-enabled-${index}`}>{t("alert.enabled")}</FieldLabel></FieldContent>
                    <Switch id={`target-enabled-${index}`} checked={target.enabled}
                      onCheckedChange={(enabled) => update(index, { enabled })} />
                  </Field>
                  <Button type="button" variant="destructive" size="icon-sm" aria-label={t("network.remove_target")}
                    title={t("network.remove_target")} onClick={() => onChange(targets.filter((_, current) => current !== index))}>
                    <Trash2Icon />
                  </Button>
                </div>
                <div className="sm:col-span-2 lg:col-span-4">
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <Badge variant="outline">{t(`network.carrier.${target.carrier}`)}</Badge>
                    {target.region ? <span>{target.region}</span> : null}
                    <span>{t(`network.type.${type}`)}</span>
                    <span>{sampleLabel(type, t)}: {target.packet_count}</span>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

function newTarget(carrier: NetworkCarrier, type: NetworkProbeType, sortOrder: number, t: (key: string) => string): NetworkTarget {
  return {
    id: "", label: t(`network.carrier.${carrier}`), carrier, region: "", target: "",
    ip_family: "auto", packet_count: type === "icmp" ? 5 : 3, visible_address: false,
    enabled: true, sort_order: sortOrder,
  }
}

export function newThreeCarrierTargets(type: NetworkProbeType, t: (key: string) => string): NetworkTarget[] {
  return ["mobile", "unicom", "telecom"].map((carrier, index) => newTarget(carrier as NetworkCarrier, type, index, t))
}

function carrierOptions(t: (key: string) => string) {
  return carriers.map((value) => ({ label: t(`network.carrier.${value}`), value }))
}

function familyOptions(t: (key: string) => string) {
  return (["auto", "ipv4", "ipv6"] as const).map((value) => ({ label: t(`network.family.${value}`), value }))
}

function sampleLabel(type: NetworkProbeType, t: (key: string) => string): string {
  return t(type === "icmp" ? "network.packet_count" : "network.attempt_count")
}
