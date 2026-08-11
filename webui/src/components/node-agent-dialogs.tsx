import { useEffect, useId, useMemo, useState } from "react"
import { CheckIcon, CopyIcon, PlusIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import type {
  AgentConfig,
  ManagedNodePayload,
  NodeCredential,
  SSHKey,
} from "@/types"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

interface Option {
  label: string
  value: string
}

interface CreateNodeDialogProps {
  groups: Option[]
  loading: boolean
  open: boolean
  sshKeys: SSHKey[]
  onOpenChange: (open: boolean) => void
  onSubmit: (payload: ManagedNodePayload) => Promise<void>
}

const emptyForm = {
  name: "",
  alias: "",
  group_id: "",
  host: "",
  port: "22",
  ssh_public_key: "",
}

export function CreateNodeDialog({
  groups,
  loading,
  open,
  sshKeys,
  onOpenChange,
  onSubmit,
}: CreateNodeDialogProps) {
  const { t } = useLocale()
  const [form, setForm] = useState(emptyForm)
  const groupItems = useMemo(() => [
    { label: t("node.not_assigned"), value: "none" },
    ...groups,
  ], [groups, t])
  const keyItems = useMemo(() => [
    { label: t("node.not_assigned"), value: "none" },
    ...sshKeys.map((key) => ({ label: key.name, value: key.public_key })),
  ], [sshKeys, t])
  const valid = form.name.trim() !== "" && form.host.trim() !== "" &&
    Number(form.port) >= 1 && Number(form.port) <= 65535

  useEffect(() => {
    if (!open) setForm(emptyForm)
  }, [open])

  const submit = async () => {
    if (!valid) return
    await onSubmit({
      name: form.name.trim(),
      alias: form.alias.trim(),
      group_id: form.group_id === "none" ? "" : form.group_id,
      host: form.host.trim(),
      port: Number(form.port),
      ssh_public_key: form.ssh_public_key === "none" ? "" : form.ssh_public_key,
      server_url: window.location.origin,
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("agent.create_node")}</DialogTitle>
          <DialogDescription>{t("agent.create_description")}</DialogDescription>
        </DialogHeader>
        <FieldGroup className="grid gap-4 sm:grid-cols-2">
          <TextField label={t("node.name")} value={form.name}
            onChange={(value) => setForm({ ...form, name: value })} />
          <TextField label={t("node.alias")} value={form.alias}
            onChange={(value) => setForm({ ...form, alias: value })} />
          <TextField label={t("node.host")} value={form.host}
            onChange={(value) => setForm({ ...form, host: value })} />
          <TextField label={t("node.port")} type="number" value={form.port}
            onChange={(value) => setForm({ ...form, port: value })} />
          <Field>
            <FieldLabel>{t("node.group")}</FieldLabel>
            <Select
              items={groupItems}
              value={form.group_id || "none"}
              onValueChange={(value) => setForm({ ...form, group_id: value ?? "none" })}
            >
              <SelectTrigger aria-label={t("node.group")}><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {groupItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{t("node.ssh_key")}</FieldLabel>
            <Select
              items={keyItems}
              value={form.ssh_public_key || "none"}
              onValueChange={(value) => setForm({ ...form, ssh_public_key: value ?? "none" })}
            >
              <SelectTrigger aria-label={t("node.ssh_key")}><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {keyItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button onClick={submit} disabled={!valid || loading}>
            <PlusIcon />{t("app.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface AgentConfigDialogProps {
  value: NodeCredential | AgentConfig | null
  onClose: () => void
}

export function AgentConfigDialog({ value, onClose }: AgentConfigDialogProps) {
  const { t } = useLocale()
  const [copied, setCopied] = useState<"token" | "config" | null>(null)
  const config = value && "agent_config" in value ? value.agent_config : value
  const token = value && "agent_token" in value ? value.agent_token : ""
  const configText = config ? JSON.stringify(config, null, 2) : ""

  useEffect(() => {
    if (!value) setCopied(null)
  }, [value])

  const copy = async (kind: "token" | "config", content: string) => {
    try {
      await navigator.clipboard.writeText(content)
      setCopied(kind)
    } catch {
      setCopied(null)
    }
  }

  return (
    <Dialog open={value !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{token ? t("agent.credentials_title") : t("agent.install_config")}</DialogTitle>
          <DialogDescription>{t("agent.credentials_description")}</DialogDescription>
        </DialogHeader>
        {token && (
          <Alert>
            <AlertDescription>{t("agent.one_time_warning")}</AlertDescription>
          </Alert>
        )}
        {token && (
          <CopyField label={t("agent.token")} value={token} copied={copied === "token"}
            onCopy={() => copy("token", token)} />
        )}
        <CopyField label={t("agent.config")} value={configText} multiline
          copied={copied === "config"} onCopy={() => copy("config", configText)} />
        <DialogFooter>
          <Button onClick={onClose}>{t("app.confirm")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TextField({
  label,
  onChange,
  type = "text",
  value,
}: {
  label: string
  onChange: (value: string) => void
  type?: string
  value: string
}) {
  const id = useId()
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input id={id} type={type} value={value} onChange={(event) => onChange(event.target.value)} />
    </Field>
  )
}

function CopyField({
  copied,
  label,
  multiline = false,
  onCopy,
  value,
}: {
  copied: boolean
  label: string
  multiline?: boolean
  onCopy: () => void
  value: string
}) {
  return (
    <Field>
      <div className="flex items-center justify-between gap-2">
        <FieldLabel>{label}</FieldLabel>
        <Button variant="ghost" size="icon-sm" onClick={onCopy} aria-label={label}>
          {copied ? <CheckIcon /> : <CopyIcon />}
        </Button>
      </div>
      {multiline ? (
        <pre className="max-h-56 overflow-auto rounded-md border bg-muted p-3 text-xs whitespace-pre-wrap break-all">
          {value}
        </pre>
      ) : (
        <code className="block rounded-md border bg-muted p-3 text-xs break-all">{value}</code>
      )}
    </Field>
  )
}
