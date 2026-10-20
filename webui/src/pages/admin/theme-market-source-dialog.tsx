import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import type { ThemeMarketInput, ThemeMarketSource } from "@/types/theme"

interface SourceDialogProps {
  source: ThemeMarketSource | "new"
  open: boolean
  onClose: () => void
  onSave: (input: ThemeMarketInput) => void
}

export function ThemeMarketSourceDialog({ source, open, onClose, onSave }: SourceDialogProps) {
  const { t } = useLocale()
  const editing = source !== "new"
  const [input, setInput] = useState<ThemeMarketInput>(() => ({ name: editing ? source.name : "", catalog_url: editing ? source.catalog_url : "",
    public_key: "", key_fingerprint: "", enabled: editing ? source.enabled : true, sort_order: editing ? source.sort_order : 0 }))
  const field = (name: keyof ThemeMarketInput, value: string | boolean) => setInput((current) => ({ ...current, [name]: value }))
  const submit = (event: FormEvent) => { event.preventDefault(); onSave(input) }
  return <Dialog open={open} onOpenChange={(value) => { if (!value) onClose() }}><DialogContent>
    <DialogHeader><DialogTitle>{t(editing ? "theme.edit_source" : "theme.add_source")}</DialogTitle>
      <DialogDescription>{t("theme.add_source_description")}</DialogDescription></DialogHeader>
    <form onSubmit={submit}><FieldGroup>
      <Field><FieldLabel htmlFor="market-name">{t("theme.source_name")}</FieldLabel>
        <Input id="market-name" value={input.name} maxLength={200} required onChange={(event) => field("name", event.target.value)} /></Field>
      <Field><FieldLabel htmlFor="market-url">{t("theme.source_url")}</FieldLabel>
        <Input id="market-url" type="url" placeholder="https://market.example/catalog.json" value={input.catalog_url} maxLength={2048} required onChange={(event) => field("catalog_url", event.target.value)} /></Field>
      <Field><FieldLabel htmlFor="market-key">{t("theme.source_key")}</FieldLabel>
        <Input id="market-key" value={input.public_key} required={!editing} maxLength={128} onChange={(event) => field("public_key", event.target.value)} />
        <FieldDescription>{t(editing ? "theme.keep_public_key" : "theme.public_key_hint")}</FieldDescription></Field>
      <Field><FieldLabel htmlFor="market-fingerprint">{t("theme.source_fingerprint")}</FieldLabel>
        <Input id="market-fingerprint" value={input.key_fingerprint} placeholder="sha256:…" maxLength={71} onChange={(event) => field("key_fingerprint", event.target.value)} />
        <FieldDescription>{t("theme.fingerprint_hint")}</FieldDescription></Field>
      <div className="flex items-center gap-2"><Switch id="market-enabled" checked={input.enabled} onCheckedChange={(value) => field("enabled", value)} />
        <Label htmlFor="market-enabled">{t("security.enabled")}</Label></div>
      <Button type="submit">{t(editing ? "theme.save_source" : "theme.add_source")}</Button>
    </FieldGroup></form>
  </DialogContent></Dialog>
}
