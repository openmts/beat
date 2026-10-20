import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { uploadMMDB, updateMMDB, rollbackMMDB, type MMDBUpdate } from "@/lib/lifecycle-api"
import type { GeoIPSettings } from "@/types/node-lifecycle"
import { OperationConfirm } from "./operation-confirm"
import { TextField, ChoiceField } from "./lifecycle-fields"
import { formatBytes } from "@/lib/metric-format"
// 数据库状态是后端契约里的取值；未知状态回落原值，避免新增状态时界面空白。
const mmdbStateKeys: Record<string, string> = { missing: "lifecycle.mmdb_state_missing", ready: "lifecycle.mmdb_state_ready" }
const mmdbActionKeys = { upload: "lifecycle.mmdb_action_upload", update: "lifecycle.mmdb_action_update", rollback: "lifecycle.mmdb_action_rollback" } as const
export function MMDBManager({ value, saved }: { value: GeoIPSettings; saved: () => void }) {
 const { t, locale } = useLocale(), flow = useConfirmedOperation()
 const [file, setFile] = useState<File | null>(null), [account, setAccount] = useState(""), [license, setLicense] = useState("")
 const [edition, setEdition] = useState<MMDBUpdate["edition"]>("GeoLite2-Country")
 const request = (kind: "upload" | "update" | "rollback") => {
  let accountID = account, licenseKey = license
  flow.request({ title: `${t("lifecycle.update_country_database")}: ${t(mmdbActionKeys[kind])}`, description: t("lifecycle.this_operation_uses_the_current_revision_and_rep"), execute: async () => {
  if (kind === "update" && !licenseKey) throw new Error(t("lifecycle.close_this_confirmation_and_enter_the_license_ke"))
  try {
   if (kind === "upload" && file) await uploadMMDB(file, value.revision)
   else if (kind === "update") await updateMMDB({ account_id: accountID, license_key: licenseKey, edition, expected_revision: value.revision })
   else if (kind === "rollback") await rollbackMMDB(value.revision)
   saved()
  } finally { licenseKey = ""; accountID = ""; setLicense(""); setAccount(""); setFile(null) }
 } })
 }
 const submit = (event: FormEvent) => { event.preventDefault(); request("update") }
 return <section className="space-y-4"><h3 className="font-medium">MMDB</h3><p className="break-all text-sm text-muted-foreground">{t("lifecycle.mmdb_status", { state: mmdbStateKeys[value.mmdb.state] ? t(mmdbStateKeys[value.mmdb.state]) : value.mmdb.state, source: value.mmdb.source || "—", size: formatBytes(value.mmdb.size_bytes, locale) })}<br />{t("lifecycle.mmdb_sha256", { digest: value.mmdb.sha256 || "—" })}</p>{value.mmdb.error_code && <p role="status">{t("lifecycle.mmdb_error", { code: value.mmdb.error_code })}</p>}
  <fieldset className="space-y-4" disabled={flow.busy}><label className="grid gap-2 text-sm">{t("lifecycle.upload_country_database_maximum_128_mib")}<Input type="file" accept=".mmdb" onChange={event => setFile(event.target.files?.[0] ?? null)} /></label>
  {file && (file.size === 0 || file.size > 128 * 1024 * 1024) && <p role="alert">{t("lifecycle.invalid_database_size")}</p>}<div className="flex gap-2"><Button disabled={!file || file.size === 0 || file.size > 128 * 1024 * 1024} onClick={() => request("upload")}>{t("lifecycle.upload")}</Button><Button variant="outline" onClick={() => request("rollback")}>{t("lifecycle.roll_back_database")}</Button></div>
  <form className="space-y-4" onSubmit={submit}><TextField label={t("lifecycle.maxmind_account_id")} value={account} onChange={setAccount} maxLength={128} /><TextField label={t("lifecycle.maxmind_transient_license_key")} type="password" value={license} onChange={setLicense} maxLength={8192} /><ChoiceField label={t("lifecycle.edition")} value={edition} onChange={v => setEdition(v as MMDBUpdate["edition"])}><option>GeoLite2-Country</option><option>GeoIP2-Country</option></ChoiceField><Button type="submit" disabled={!account || !license}>{t("lifecycle.download_from_maxmind")}</Button></form></fieldset><OperationConfirm flow={flow} />
 </section>
}
