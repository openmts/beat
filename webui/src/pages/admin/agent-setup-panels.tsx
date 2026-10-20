import { useState, type ReactNode } from "react"
import { CheckCircle2Icon, KeyRoundIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import {
  agentArchitectures, agentConfigFields, agentServiceUnit,
  buildAgentCommand, buildAgentConfig, buildHealthCommand,
  enableAgentServiceCommand, installAgentCommand, prepareAgentServiceCommand,
  restartAgentServiceCommand, startAgentCommand, type AgentArchitecture,
} from "@/lib/agent-setup"
import { AgentSetupCode } from "@/components/agent-setup-code"
import {
  useAgentOptionalSections, useAgentOptionalText, type ResolvedAgentOptionalSection,
} from "@/pages/admin/agent-setup-optional-fields"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

interface ServerProps { serverURL: string | null }

export function AgentInstallGuide({ serverURL }: ServerProps) {
  const { t } = useLocale()
  return (
    <div className="grid min-w-0 gap-5">
      <GuideStep number="1" title={t("setup.create_title")}>
        <p>{t("setup.create_body")}</p>
        <Alert><KeyRoundIcon /><AlertDescription>{t("setup.token_hint")}</AlertDescription></Alert>
      </GuideStep>
      <InstallStep />
      <GuideStep number="3" title={t("setup.configure_title")}>
        <p>{t("setup.configure_body")}</p>
        {serverURL && <AgentSetupCode title="agent.json" code={buildAgentConfig(serverURL)} />}
        <p>{t("setup.start_hint")}</p>
        {serverURL && <AgentSetupCode title={t("setup.start_label")} code={startAgentCommand} />}
      </GuideStep>
      <div className="flex items-start gap-3 rounded-xl border p-4">
        <CheckCircle2Icon className="mt-0.5 size-5 shrink-0 text-success" />
        <div className="space-y-1">
          <h3 className="font-medium">{t("setup.verify_title")}</h3>
          <p className="text-sm leading-relaxed text-muted-foreground">{t("setup.verify_body")}</p>
        </div>
      </div>
    </div>
  )
}

function InstallStep() {
  const { t } = useLocale()
  const [architecture, setArchitecture] = useState<AgentArchitecture>("amd64")
  return (
    <GuideStep number="2" title={t("setup.install_title")}>
      <p>{t("setup.install_body")}</p>
      <div className="flex flex-wrap items-center gap-3">
        <Label htmlFor="setup-architecture">{t("setup.architecture")}</Label>
        <Select items={agentArchitectures} value={architecture}
          onValueChange={(value) => setArchitecture(value === "arm64" ? "arm64" : "amd64")}>
          <SelectTrigger id="setup-architecture" aria-label={t("setup.architecture")}><SelectValue /></SelectTrigger>
          <SelectContent>
            {agentArchitectures.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>
      <AgentSetupCode title={t("setup.build_label")} code={buildAgentCommand(architecture)} />
      <p>{t("setup.transfer_hint")}</p>
      <AgentSetupCode title={t("setup.install_label")} code={installAgentCommand} />
    </GuideStep>
  )
}

function GuideStep({ number, title, children }: { number: string; title: string; children: ReactNode }) {
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-3">
          <span className="grid size-7 shrink-0 place-items-center rounded-md bg-primary font-mono text-sm text-primary-foreground">{number}</span>
          <h3>{title}</h3>
        </CardTitle>
      </CardHeader>
      <CardContent className="min-w-0 space-y-4 text-sm leading-relaxed [&>p]:text-muted-foreground">{children}</CardContent>
    </Card>
  )
}

export function AgentFieldReference() {
  const { t } = useLocale()
  return (
    <div className="grid min-w-0 gap-5">
      <Card>
        <CardHeader>
          <CardTitle><h3>{t("setup.fields_title")}</h3></CardTitle>
          <CardDescription>{t("setup.fields_description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Table aria-label={t("setup.fields_title")}>
            <TableHeader><TableRow>
              <TableHead>{t("setup.field")}</TableHead>
              <TableHead>{t("setup.requirement")}</TableHead>
              <TableHead>{t("setup.meaning")}</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {agentConfigFields.map((field) => (
                <TableRow key={field.name}>
                  <TableCell className="align-top">
                    <code className="font-medium">{field.name}</code>
                    <div className="mt-1 font-mono text-xs text-muted-foreground">{field.type}</div>
                  </TableCell>
                  <TableCell className="min-w-36 align-top whitespace-normal text-muted-foreground">{t(`setup.${field.requirement}`)}</TableCell>
                  <TableCell className="min-w-64 align-top leading-relaxed whitespace-normal">{t(`setup.field_${field.name}`)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle><h3>{t("setup.constraints_title")}</h3></CardTitle></CardHeader>
        <CardContent><ul className="list-disc space-y-3 pl-5 text-sm leading-relaxed text-muted-foreground">
          {["json_rule", "permission_rule", "restart_rule"].map((key) => <li key={key}>{t(`setup.${key}`)}</li>)}
        </ul></CardContent>
      </Card>
      <AgentOptionalFields />
    </div>
  )
}

export function AgentOptionalFields() {
  const text = useAgentOptionalText()
  const sections = useAgentOptionalSections()
  return (
    <Card>
      <CardHeader>
        <CardTitle><h3>{text.title}</h3></CardTitle>
        <CardDescription>{text.description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        {sections.map((section) => <AgentOptionalSectionTable key={section.name} section={section} />)}
      </CardContent>
    </Card>
  )
}

function AgentOptionalSectionTable({ section }: { section: ResolvedAgentOptionalSection }) {
  const { t } = useLocale()
  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h4 className="font-mono text-sm font-medium">{section.name}</h4>
        <p className="text-sm leading-relaxed text-muted-foreground">{section.meaning}</p>
      </div>
      <Table aria-label={section.name}>
        <TableHeader><TableRow>
          <TableHead>{t("setup.field")}</TableHead>
          <TableHead>{t("setup.requirement")}</TableHead>
          <TableHead>{t("setup.meaning")}</TableHead>
        </TableRow></TableHeader>
        <TableBody>
          {section.fields.map((field) => (
            <TableRow key={field.name}>
              <TableCell className="align-top">
                <code className="font-medium">{field.name}</code>
                <div className="mt-1 font-mono text-xs text-muted-foreground">{field.type}</div>
              </TableCell>
              <TableCell className="min-w-36 align-top whitespace-normal text-muted-foreground">{field.requirement}</TableCell>
              <TableCell className="min-w-64 align-top leading-relaxed whitespace-normal">{field.meaning}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  )
}

export function AgentServiceGuide({ serverURL }: ServerProps) {
  const { t } = useLocale()
  return (
    <div className="grid min-w-0 items-start gap-5 xl:grid-cols-2">
      <Card className="min-w-0">
        <CardHeader>
          <CardTitle><h3>{t("setup.service_title")}</h3></CardTitle>
          <CardDescription>{t("setup.service_description")}</CardDescription>
        </CardHeader>
        <CardContent className="min-w-0 space-y-4 text-sm leading-relaxed [&>p]:text-muted-foreground">
          <p>{t("setup.service_prepare")}</p>
          <AgentSetupCode title={t("setup.service_prepare_label")} code={prepareAgentServiceCommand} />
          <AgentSetupCode title={t("setup.service_unit_label")} code={agentServiceUnit} />
          <p>{t("setup.service_user")}</p>
          <AgentSetupCode title={t("setup.service_enable_label")} code={enableAgentServiceCommand} />
          <p>{t("setup.service_restart_hint")}</p>
          <AgentSetupCode title={t("setup.service_restart_label")} code={restartAgentServiceCommand} />
        </CardContent>
      </Card>
      <Troubleshooting serverURL={serverURL} />
    </div>
  )
}

function Troubleshooting({ serverURL }: ServerProps) {
  const { t } = useLocale()
  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle><h3>{t("setup.troubleshoot_title")}</h3></CardTitle>
        <CardDescription>{t("setup.health_hint")}</CardDescription>
      </CardHeader>
      <CardContent className="min-w-0 space-y-5">
        {serverURL && <AgentSetupCode title={t("setup.health_label")} code={buildHealthCommand(serverURL)} />}
        <dl className="space-y-5 text-sm leading-relaxed">
          {["auth", "config", "network", "offline"].map((key) => (
            <div key={key} className="space-y-1">
              <dt className="font-medium">{t(`setup.failure_${key}_title`)}</dt>
              <dd className="text-muted-foreground">{t(`setup.failure_${key}`)}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}
