import { useState } from "react"
import { Link } from "react-router"
import { ArrowRightIcon, BookOpenIcon, KeyRoundIcon, ServerIcon, Settings2Icon, TerminalIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { normalizeServerURL } from "@/lib/agent-setup"
import { PageHeader } from "@/components/page-header"
import { buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AgentFieldReference, AgentInstallGuide, AgentServiceGuide } from "@/pages/admin/agent-setup-panels"
import { AgentEnrollment } from "@/pages/admin/agent-enrollment"

export default function AgentSetup() {
  const { t } = useLocale()
  const [address, setAddress] = useState(window.location.origin)
  const serverURL = normalizeServerURL(address)

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <PageHeader title={t("setup.title")} description={t("setup.description")}>
        <Link to="/admin/nodes" className={buttonVariants()}>
          <ServerIcon />{t("setup.manage_nodes")}<ArrowRightIcon />
        </Link>
      </PageHeader>
      <ServerAddress value={address} onChange={setAddress} valid={serverURL !== null} />
      <Tabs defaultValue="join" className="min-w-0 gap-5">
        <TabsList className="h-auto! max-w-full flex-wrap justify-start">
          <TabsTrigger value="join"><KeyRoundIcon />{t("enrollment.tab")}</TabsTrigger>
          <TabsTrigger value="install"><TerminalIcon />{t("setup.install_tab")}</TabsTrigger>
          <TabsTrigger value="fields"><BookOpenIcon />{t("setup.fields_tab")}</TabsTrigger>
          <TabsTrigger value="service"><Settings2Icon />{t("setup.service_tab")}</TabsTrigger>
        </TabsList>
        <TabsContent value="join" keepMounted><AgentEnrollment key={serverURL} serverURL={serverURL} /></TabsContent>
        <TabsContent value="install" keepMounted><AgentInstallGuide serverURL={serverURL} /></TabsContent>
        <TabsContent value="fields"><AgentFieldReference /></TabsContent>
        <TabsContent value="service"><AgentServiceGuide serverURL={serverURL} /></TabsContent>
      </Tabs>
    </div>
  )
}

function ServerAddress({ value, onChange, valid }: {
  value: string
  onChange: (value: string) => void
  valid: boolean
}) {
  const { t } = useLocale()
  return (
    <div className="grid gap-3 rounded-xl border bg-muted/30 p-4 lg:grid-cols-[minmax(15rem,1fr)_2fr] lg:items-center">
      <div className="space-y-2">
        <Label htmlFor="setup-server-url">{t("setup.server_url")}</Label>
        <Input id="setup-server-url" type="url" value={value} spellCheck={false}
          onChange={(event) => onChange(event.target.value)} aria-invalid={!valid}
          aria-describedby={`setup-server-hint${valid ? "" : " setup-server-error"}`}
          className="bg-background font-mono text-sm" />
      </div>
      <div className="space-y-2 text-sm leading-relaxed">
        <p id="setup-server-hint" className="text-muted-foreground">{t("setup.server_hint")}</p>
        {!valid && <p id="setup-server-error" role="alert" className="text-destructive">{t("setup.server_invalid")}</p>}
      </div>
    </div>
  )
}
