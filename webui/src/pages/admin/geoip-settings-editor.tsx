import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { saveGeoIPSettings } from "@/lib/lifecycle-api"
import type { GeoIPSettings } from "@/types/node-lifecycle"
import { OperationConfirm } from "./operation-confirm"
import { TextField, ChoiceField, CheckField } from "./lifecycle-fields"
import { MMDBManager } from "./mmdb-manager"
// provider 是持久化契约里的枚举，界面只负责把它翻成人类可读的名字；未知取值回落原值，避免新增 provider 时空白。
const providerLabelKeys: Record<string, string> = {
 disabled: "lifecycle.provider_disabled", mmdb: "lifecycle.provider_mmdb",
 geojs_https: "lifecycle.provider_geojs_https", ipinfo_https: "lifecycle.provider_ipinfo_https",
 ip_api_pro_https: "lifecycle.provider_ip_api_pro_https",
}
export function GeoIPSettingsEditor({ value, saved }: { value: GeoIPSettings; saved: () => void }) {
 const { t } = useLocale(), flow = useConfirmedOperation()
 const [provider, setProvider] = useState(value.provider), [consent, setConsent] = useState(false)
 const [credential, setCredential] = useState(""), [clearCredential, setClearCredential] = useState(false), [purge, setPurge] = useState(false)
 const remote = !["disabled", "mmdb"].includes(provider)
 const providerLabel = (name: string) => (providerLabelKeys[name] ? t(providerLabelKeys[name]) : name)
 const submit = (event: FormEvent) => {
  event.preventDefault()
  if (remote && !consent) return
  let secret = credential
  const replacingCredential = Boolean(secret)
  flow.request({ title: t("lifecycle.update_geoip_settings"), description: t("lifecycle.remote_providers_receive_ip_addresses_for_lookup"), execute: async () => {
   if (replacingCredential && !secret) throw new Error(t("lifecycle.close_this_confirmation_and_enter_the_credential"))
   try { await saveGeoIPSettings({ provider, remote_consent: remote && consent, expected_revision: value.revision, purge, ...(clearCredential ? { credential: "" } : secret ? { credential: secret } : {}) }); saved() }
   finally { secret = ""; setCredential("") }
  } })
 }
 return <section className="space-y-6"><form className="space-y-4" onSubmit={submit}><fieldset className="space-y-4" disabled={flow.busy}>
  <h2 className="font-semibold">GeoIP</h2><ChoiceField label={t("lifecycle.provider")} value={provider} onChange={v => { setProvider(v); setConsent(false); setCredential("") }}>{["disabled", "mmdb", "geojs_https", "ipinfo_https", "ip_api_pro_https"].map(name => <option key={name} value={name}>{providerLabel(name)}</option>)}</ChoiceField>
  {remote && <CheckField checked={consent} onChange={setConsent} label={t("lifecycle.remote_consent", { provider: providerLabel(provider) })} />}
  {(provider === "ipinfo_https" || provider === "ip_api_pro_https") && <><TextField label={t("lifecycle.new_credential_blank_preserves_existing_value")} type="password" value={credential} onChange={setCredential} maxLength={8192} /><p className="text-sm text-muted-foreground">{value.has_credential ? (t("lifecycle.credential_configured")) : (t("lifecycle.no_credential_configured"))}</p><CheckField checked={clearCredential} onChange={setClearCredential} label={t("lifecycle.clear_existing_credential")} /></>}
  <CheckField checked={purge} onChange={setPurge} label={t("lifecycle.also_purge_location_cache_and_sources")} /><Button type="submit" disabled={remote && !consent}>{t("lifecycle.save_geoip_settings")}</Button>
 </fieldset></form><MMDBManager value={value} saved={saved} /><OperationConfirm flow={flow} /></section>
}
