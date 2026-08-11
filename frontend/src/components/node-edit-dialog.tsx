import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { parseNodeTags, toNodeUpdatePayload, type EditableNode, type NodeUpdatePayload } from "@/lib/node-traffic"
import type { SSHKey, TrafficLimitType } from "@/types"

interface NodeEditDialogProps {
  node: EditableNode | null
  groups: ReadonlyArray<{ label: string; value: string }>
  sshKeys: SSHKey[]
  loading: boolean
  onChange: (node: EditableNode | null) => void
  onSave: (payload: NodeUpdatePayload) => void
}

export function NodeEditDialog({ node, groups, sshKeys, loading, onChange, onSave }: NodeEditDialogProps) {
  const { t } = useLocale()
  const error = node ? validateEditableNode(node, t) : null
  const trafficTypes = trafficTypeOptions(t)
  const sshOptions = [
    { label: t("node.not_assigned"), value: "none" },
    ...sshKeys.map((key) => ({ label: key.name, value: key.id })),
  ]
  const selectedSSHKey = node?.sshPublicKey
    ? sshKeys.find((key) => key.public_key === node.sshPublicKey)?.id ?? "none"
    : "none"

  return (
    <Dialog open={node !== null} onOpenChange={(open) => { if (!open) onChange(null) }}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("app.edit")} {t("app.nodes")}</DialogTitle>
          <DialogDescription>{t("traffic.limit_hint")}</DialogDescription>
        </DialogHeader>
        {node ? (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="node-alias">{t("node.alias")}</FieldLabel>
              <Input id="node-alias" value={node.alias} onChange={(event) => onChange({ ...node, alias: event.target.value })} />
            </Field>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="node-group">{t("node.group")}</FieldLabel>
                <Select items={groups} value={node.groupId} onValueChange={(value) => onChange({ ...node, groupId: value ?? "" })}>
                  <SelectTrigger id="node-group"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>{groups.map((group) => (
                    <SelectItem key={group.value} value={group.value}>{group.label}</SelectItem>
                  ))}</SelectGroup></SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="node-ssh-key">{t("node.ssh_key")}</FieldLabel>
                <Select items={sshOptions} value={selectedSSHKey} onValueChange={(value) => onChange({
                  ...node,
                  sshPublicKey: value === "none" ? "" : sshKeys.find((key) => key.id === value)?.public_key ?? "",
                })}>
                  <SelectTrigger id="node-ssh-key"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>{sshOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}</SelectGroup></SelectContent>
                </Select>
              </Field>
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldLabel htmlFor="node-public">{t("node.public_visibility")}</FieldLabel>
                  <FieldDescription>{t("node.public_visibility_hint")}</FieldDescription>
                </FieldContent>
                <Switch
                  id="node-public"
                  aria-label={t("node.public_visibility")}
                  checked={node.isPublic}
                  onCheckedChange={(checked) => onChange({ ...node, isPublic: checked })}
                />
              </Field>
              <Field data-invalid={Boolean(error?.sortOrder)}>
                <FieldLabel htmlFor="node-sort-order">{t("node.sort_order")}</FieldLabel>
                <Input
                  id="node-sort-order"
                  type="number"
                  min="0"
                  step="1"
                  aria-invalid={Boolean(error?.sortOrder)}
                  value={node.sortOrder}
                  onChange={(event) => onChange({ ...node, sortOrder: event.target.value })}
                />
                <FieldError>{error?.sortOrder}</FieldError>
              </Field>
            </div>
            <Field data-invalid={Boolean(error?.tags)}>
              <FieldLabel htmlFor="node-tags">{t("node.tags")}</FieldLabel>
              <Input
                id="node-tags"
                aria-invalid={Boolean(error?.tags)}
                value={node.tags}
                onChange={(event) => onChange({ ...node, tags: event.target.value })}
              />
              <FieldDescription>{t("node.tags_hint")}</FieldDescription>
              <FieldError>{error?.tags}</FieldError>
            </Field>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field data-invalid={Boolean(error?.publicRemark)}>
                <FieldLabel htmlFor="node-public-remark">{t("node.public_remark")}</FieldLabel>
                <Textarea
                  id="node-public-remark"
                  maxLength={500}
                  aria-invalid={Boolean(error?.publicRemark)}
                  value={node.publicRemark}
                  onChange={(event) => onChange({ ...node, publicRemark: event.target.value })}
                />
                <FieldError>{error?.publicRemark}</FieldError>
              </Field>
              <Field data-invalid={Boolean(error?.privateRemark)}>
                <FieldLabel htmlFor="node-private-remark">{t("node.private_remark")}</FieldLabel>
                <Textarea
                  id="node-private-remark"
                  maxLength={2000}
                  aria-invalid={Boolean(error?.privateRemark)}
                  value={node.privateRemark}
                  onChange={(event) => onChange({ ...node, privateRemark: event.target.value })}
                />
                <FieldDescription>{t("node.private_remark_hint")}</FieldDescription>
                <FieldError>{error?.privateRemark}</FieldError>
              </Field>
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field data-invalid={Boolean(error?.limit)}>
                <FieldLabel htmlFor="traffic-limit">{t("traffic.limit")} (GiB)</FieldLabel>
                <Input id="traffic-limit" type="number" min="0" step="0.01" aria-invalid={Boolean(error?.limit)} value={node.trafficLimitGiB} onChange={(event) => onChange({ ...node, trafficLimitGiB: event.target.value })} />
                <FieldDescription>{t("traffic.limit_hint")}</FieldDescription>
                <FieldError>{error?.limit}</FieldError>
              </Field>
              <Field>
                <FieldLabel htmlFor="traffic-type">{t("traffic.limit_type")}</FieldLabel>
                <Select items={trafficTypes} value={node.trafficLimitType} onValueChange={(value) => onChange({ ...node, trafficLimitType: (value ?? "sum") as TrafficLimitType })}>
                  <SelectTrigger id="traffic-type"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>{trafficTypes.map((option) => (
                    <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
                  ))}</SelectGroup></SelectContent>
                </Select>
              </Field>
            </div>
            <Field data-invalid={Boolean(error?.resetDay)}>
              <FieldLabel htmlFor="traffic-reset-day">{t("traffic.reset_day")}</FieldLabel>
              <Input id="traffic-reset-day" type="number" min="1" max="31" step="1" aria-invalid={Boolean(error?.resetDay)} value={node.trafficResetDay} onChange={(event) => onChange({ ...node, trafficResetDay: event.target.value })} />
              <FieldDescription>{t("traffic.reset_day_hint")}</FieldDescription>
              <FieldError>{error?.resetDay}</FieldError>
            </Field>
          </FieldGroup>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={() => onChange(null)}>{t("app.cancel")}</Button>
          <Button disabled={loading || Boolean(error)} onClick={() => { if (node && !error) onSave(toNodeUpdatePayload(node)) }}>{t("app.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function validateEditableNode(node: EditableNode, t: (key: string) => string) {
  const limit = Number(node.trafficLimitGiB)
  const resetDay = Number(node.trafficResetDay)
  const sortOrder = Number(node.sortOrder)
  const tags = parseNodeTags(node.tags)
  const errors = {
    limit: Number.isFinite(limit) && limit >= 0 ? null : t("traffic.invalid_limit"),
    resetDay: Number.isInteger(resetDay) && resetDay >= 1 && resetDay <= 31
      ? null
      : t("traffic.invalid_reset_day"),
    sortOrder: Number.isInteger(sortOrder) && sortOrder >= 0 ? null : t("node.invalid_sort_order"),
    tags: tags.length <= 12 && tags.every((tag) => Array.from(tag).length <= 32)
      ? null
      : t("node.invalid_tags"),
    publicRemark: Array.from(node.publicRemark).length <= 500 ? null : t("node.invalid_public_remark"),
    privateRemark: Array.from(node.privateRemark).length <= 2000 ? null : t("node.invalid_private_remark"),
  }
  return Object.values(errors).some(Boolean) ? errors : null
}

function trafficTypeOptions(t: (key: string) => string) {
  return (["up", "down", "sum", "min", "max"] as TrafficLimitType[]).map((value) => ({
    value,
    label: t(`traffic.type.${value}`),
  }))
}
