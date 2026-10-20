import type { ReactNode } from "react"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { useLocale } from "@/context/locale"
export function TextField({ label, value, onChange, type = "text", maxLength }: { label: string; value: string; onChange: (value: string) => void; type?: string; maxLength?: number }) {
 return <label className="grid gap-2 text-sm">{label}<Input type={type} value={value} maxLength={maxLength} onChange={event => onChange(event.target.value)} /></label>
}
export function ChoiceField({ label, value, onChange, children }: { label: string; value: string; onChange: (value: string) => void; children: ReactNode }) {
 return <label className="grid gap-2 text-sm">{label}<select className="h-9 rounded-md border border-input bg-background px-3" value={value} onChange={event => onChange(event.target.value)}>{children}</select></label>
}
export function CheckField({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
 return <label className="flex items-start gap-2 text-sm"><Checkbox checked={checked} onCheckedChange={value => onChange(value === true)} />{label}</label>
}
export function VisibilityOptions() {
 const { t } = useLocale()
 return <><option value="inherit">{t("lifecycle.inherit_global")}</option><option value="public">{t("lifecycle.public")}</option><option value="private">{t("lifecycle.private")}</option></>
}
