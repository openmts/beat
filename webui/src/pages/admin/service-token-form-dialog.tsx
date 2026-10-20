import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { serviceResourceLabel, serviceScopeLabel } from "@/lib/service-token-labels"
import { serviceScopes, type ServiceResourceKind, type ServiceScope, type ServiceToken, type ServiceTokenRotationInput } from "@/types/service-tokens"

export interface ServiceTokenOption {
  id: string
  name: string
}

const expiryOptions = [1, 7, 30, 90, 365]

export function ServiceTokenFormDialog({ mode, token, nodes, groups, open, onCancel, onSubmit }: {
  mode: "create" | "rotate"
  token?: ServiceToken
  nodes: ServiceTokenOption[]
  groups: ServiceTokenOption[]
  open: boolean
  onCancel: () => void
  onSubmit: (input: ServiceTokenRotationInput) => void
}) {
  const { t } = useLocale()
  const [name, setName] = useState(token?.name ?? "")
  const [purpose, setPurpose] = useState(token?.purpose ?? "")
  const [days, setDays] = useState("30")
  const [visibility, setVisibility] = useState<"public" | "private">(token?.visibility ?? "private")
  const [scopes, setScopes] = useState<ServiceScope[]>(token?.scopes ?? [])
  const [nodeIDs, setNodeIDs] = useState<string[]>([])
  const [groupIDs, setGroupIDs] = useState<string[]>([])
  const [sources, setSources] = useState((token?.source_cidrs ?? []).join("\n"))
  const [overlap, setOverlap] = useState("0")
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const lifetime = Number(days)
    onSubmit({
      name: name.trim(),
      purpose: purpose.trim(),
      scopes,
      resources: [
        ...nodeIDs.map(id => ({ kind: "node" as ServiceResourceKind, id })),
        ...groupIDs.map(id => ({ kind: "group" as ServiceResourceKind, id })),
      ],
      visibility,
      source_cidrs: sources.split("\n").map(value => value.trim()).filter(Boolean),
      expires_at: new Date(Date.now() + lifetime * 86_400_000).toISOString(),
      extended: lifetime > 90,
      overlap_seconds: Number(overlap),
    })
  }
  const toggle = (list: string[], value: string) =>
    list.includes(value) ? list.filter(entry => entry !== value) : [...list, value]
  return <Dialog open={open} onOpenChange={next => { if (!next) onCancel() }}>
    <DialogContent className="max-h-[90vh] overflow-auto sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>{mode === "create" ? t("security.tokens.create") : t("security.tokens.rotate")}</DialogTitle>
        <DialogDescription>{mode === "create" ? t("security.tokens.create_help") : t("security.tokens.rotate_help")}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit}><FieldGroup>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field><FieldLabel htmlFor="service-token-name">{t("security.tokens.name")}</FieldLabel>
            <Input id="service-token-name" required maxLength={128} value={name} onChange={event => setName(event.target.value)} /></Field>
          <Field><FieldLabel htmlFor="service-token-purpose">{t("security.tokens.purpose")}</FieldLabel>
            <Input id="service-token-purpose" maxLength={512} value={purpose} onChange={event => setPurpose(event.target.value)} /></Field>
          <Field><FieldLabel htmlFor="service-token-expiry">{t("security.tokens.lifetime")}</FieldLabel>
            <Select items={expiryOptions.map(value => ({ value: String(value), label: `${value} ${t("security.tokens.days")}` }))}
              value={days} onValueChange={value => setDays(value ?? "30")}>
              <SelectTrigger id="service-token-expiry"><SelectValue /></SelectTrigger>
              <SelectContent>{expiryOptions.map(value => <SelectItem key={value} value={String(value)}>{value} {t("security.tokens.days")}</SelectItem>)}</SelectContent>
            </Select></Field>
          <Field><FieldLabel htmlFor="service-token-visibility">{t("security.tokens.visibility")}</FieldLabel>
            <Select items={[{ value: "private", label: t("security.tokens.visibility_private") }, { value: "public", label: t("security.tokens.visibility_public") }]}
              value={visibility} onValueChange={value => setVisibility(value === "public" ? "public" : "private")}>
              <SelectTrigger id="service-token-visibility"><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="private">{t("security.tokens.visibility_private")}</SelectItem>
                <SelectItem value="public">{t("security.tokens.visibility_public")}</SelectItem></SelectContent>
            </Select></Field>
        </div>
        <Field><FieldLabel>{t("security.tokens.scopes")}</FieldLabel>
          <div className="grid gap-2 sm:grid-cols-2">
            {serviceScopes.map(scope => <label key={scope} className="flex items-center gap-2 text-sm">
              <Checkbox checked={scopes.includes(scope)} aria-label={serviceScopeLabel(scope, t)}
                onCheckedChange={() => setScopes(current => toggle(current, scope) as ServiceScope[])} />
              <span>{serviceScopeLabel(scope, t)}</span><code className="text-xs text-muted-foreground">{scope}</code>
            </label>)}
          </div></Field>
        {[["node", nodes], ["group", groups]].map(([kind, options]) => (options as ServiceTokenOption[]).length === 0 ? null :
          <Field key={kind as string}><FieldLabel>{serviceResourceLabel(kind as ServiceResourceKind, t)}</FieldLabel>
            <div className="grid gap-2 sm:grid-cols-2">
              {(options as ServiceTokenOption[]).slice(0, 24).map(option => <label key={option.id} className="flex items-center gap-2 text-sm">
                <Checkbox checked={(kind === "node" ? nodeIDs : groupIDs).includes(option.id)} aria-label={`${serviceResourceLabel(kind as ServiceResourceKind, t)} ${option.name}`}
                  onCheckedChange={() => kind === "node" ? setNodeIDs(current => toggle(current, option.id)) : setGroupIDs(current => toggle(current, option.id))} />
                <span>{option.name}</span></label>)}
            </div></Field>)}
        <Field><FieldLabel htmlFor="service-token-sources">{t("security.tokens.source")}</FieldLabel>
          <Textarea id="service-token-sources" rows={3} placeholder="192.0.2.0/24" value={sources} onChange={event => setSources(event.target.value)} />
          <p className="text-xs text-muted-foreground">{t("security.tokens.source_help")}</p></Field>
        {mode === "rotate" && <Field><FieldLabel htmlFor="service-token-overlap">{t("security.tokens.overlap")}</FieldLabel>
          <Select items={[{ value: "0", label: t("security.tokens.overlap_none") }, { value: "3600", label: "1h" }, { value: "86400", label: "24h" }]}
            value={overlap} onValueChange={value => setOverlap(value ?? "0")}>
            <SelectTrigger id="service-token-overlap"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value="0">{t("security.tokens.overlap_none")}</SelectItem>
              <SelectItem value="3600">1h</SelectItem><SelectItem value="86400">24h</SelectItem></SelectContent>
          </Select></Field>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={!name.trim() || scopes.length === 0}>{t("security.tokens.confirm")}</Button>
          <Button type="button" variant="outline" onClick={onCancel}>{t("app.cancel")}</Button>
        </div>
      </FieldGroup></form>
    </DialogContent>
  </Dialog>
}
