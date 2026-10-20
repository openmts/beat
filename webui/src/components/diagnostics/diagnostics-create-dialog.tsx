import { useEffect, useMemo, useState } from "react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import * as diagnosticsApi from "@/lib/diagnostics-api"
import type {
  DiagnosticCapability,
  DiagnosticDirection,
  DiagnosticJob,
  DiagnosticKind,
  DiagnosticPolicy,
  DiagnosticProtocol,
  DiagnosticTarget,
} from "@/lib/diagnostics-api"

interface DiagnosticsCreateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  policy: DiagnosticPolicy | null
  targets: DiagnosticTarget[]
  capabilities: DiagnosticCapability[]
  nodes: { id: string; name: string }[]
  onCreated: (job: DiagnosticJob) => Promise<void> | void
}

const bandwidthConfirmation = "bandwidth-impact-accepted"

// bounded 把输入夹到策略与目标上限之间，避免提交必然被服务端拒绝的值。
function bounded(value: number, maximum: number, minimum: number): number {
  if (!Number.isFinite(value)) return minimum
  return Math.min(Math.max(Math.floor(value), minimum), maximum)
}

function maximumOf(candidate: number | undefined, fallback: number): number {
  return candidate !== undefined && candidate > 0 ? candidate : fallback
}

export function DiagnosticsCreateDialog(props: DiagnosticsCreateDialogProps) {
  const { t } = useLocale()
  const defaults = useMemo(() => diagnosticDefaults(), [])
  const [kind, setKind] = useState<DiagnosticKind>("mtr")
  const [nodeId, setNodeId] = useState("")
  const [targetId, setTargetId] = useState("")
  const [protocol, setProtocol] = useState<DiagnosticProtocol>("icmp")
  const [direction, setDirection] = useState<DiagnosticDirection>("forward")
  const [values, setValues] = useState(defaults)
  const [confirmed, setConfirmed] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const readyNodes = useMemo(() => {
    const ready = new Set(props.capabilities.filter((item) => item.state === "ready" && item.kinds.includes(kind)).map((item) => item.node_id))
    return props.nodes.filter((node) => ready.has(node.id)).map((node) => ({ label: node.name, value: node.id }))
  }, [props.capabilities, props.nodes, kind])
  const targetOptions = useMemo(
    () => props.targets.filter((target) => target.enabled && target.kinds.includes(kind)).map((target) => ({ label: target.name, value: target.id })),
    [props.targets, kind],
  )
  const target = targetOptions.find((option) => option.value === targetId) ? props.targets.find((item) => item.id === targetId) ?? null : null
  const maxima = useMemo(() => diagnosticMaxima(props.policy, target), [props.policy, target])
  const { probes: probeMaximum, hopRange: hopMaximum, duration: durationMaximum, streams: streamMaximum, bitrate: bitrateMaximum } = maxima
  const protocols = useMemo(() => (kind === "mtr" ? ["icmp", "tcp"] : ["tcp"]).concat(props.policy?.allow_udp ? ["udp"] : []), [kind, props.policy])
  const directions = useMemo(() => (["forward"] as string[]).concat(props.policy?.allow_reverse ? ["reverse"] : []), [props.policy])

  useEffect(() => {
    if (kind === "mtr") {
      setProtocol((current) => (current === "udp" ? "icmp" : current === "tcp" ? "tcp" : "icmp"))
      setDirection("forward")
    } else {
      setProtocol((current) => (current === "udp" ? "udp" : "tcp"))
      setDirection((current) => (current === "reverse" && !props.policy?.allow_reverse ? "forward" : current))
    }
  }, [kind, props.policy])

  // 目标或策略上限变化时收窄已填写的值，避免表单停留在必然被服务端拒绝的状态。
  useEffect(() => {
    setValues((current) => ({
      probes: Math.min(current.probes, probeMaximum),
      hopRange: Math.min(current.hopRange, hopMaximum),
      duration: Math.min(current.duration, durationMaximum),
      streams: Math.min(current.streams, streamMaximum),
      bitrate: Math.min(current.bitrate, bitrateMaximum),
    }))
  }, [probeMaximum, hopMaximum, durationMaximum, streamMaximum, bitrateMaximum])

  useEffect(() => {
    if (!readyNodes.some((option) => option.value === nodeId)) setNodeId(readyNodes[0]?.value ?? "")
  }, [readyNodes, nodeId])

  useEffect(() => {
    if (!targetOptions.some((option) => option.value === targetId)) setTargetId(targetOptions[0]?.value ?? "")
  }, [targetOptions, targetId])

  // 带宽类上限只约束 iperf3 作业，否则 MTR 会被与本作业无关的目标上限挡住。
  const bitrateInvalid = kind === "iperf3" && values.bitrate > maxima.bitrate
  const durationInvalid = kind === "iperf3" && values.duration > maxima.duration
  const kindAllowed = props.policy?.allowed_kinds.includes(kind) ?? false
  const submitDisabled = submitting || !nodeId || !targetId || !kindAllowed || bitrateInvalid || durationInvalid ||
    (kind === "iperf3" && !confirmed)

  async function submit() {
    if (submitDisabled) return
    setSubmitting(true)
    setError(null)
    try {
      const created = await diagnosticsApi.createDiagnosticJob({
        source_node_id: nodeId,
        target_id: targetId,
        kind,
        protocol,
        direction: kind === "iperf3" ? direction : "forward",
        ip_family: target?.ip_family ?? "ipv4",
        probes: bounded(values.probes, maxima.probes, 3),
        hop_limit: bounded(values.hopRange, maxima.hopRange, 1),
        duration_seconds: kind === "iperf3" ? bounded(values.duration, maxima.duration, 3) : 0,
        parallel_streams: kind === "iperf3" ? bounded(values.streams, maxima.streams, 1) : 0,
        bitrate_mbps: kind === "iperf3" ? bounded(values.bitrate, maxima.bitrate, 1) : 0,
        confirmation: kind === "iperf3" ? bandwidthConfirmation : undefined,
      })
      await props.onCreated(created)
      props.onOpenChange(false)
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : t("diagnostics.action_failed"))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("diagnostics.create_title")}</DialogTitle>
          <DialogDescription>{t("diagnostics.create_description")}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel>{t("diagnostics.field_kind")}</FieldLabel>
            <ToggleGroup variant="outline" spacing={0} value={[kind]} onValueChange={(value) => value[0] && setKind(value[0] as DiagnosticKind)}>
              {(["mtr", "iperf3"] as DiagnosticKind[]).map((option) => (
                <ToggleGroupItem key={option} value={option} disabled={!props.policy?.allowed_kinds.includes(option)} aria-label={t(`diagnostics.kind.${option}`)}>
                  {t(`diagnostics.kind.${option}`)}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </Field>
          <Field>
            <FieldLabel htmlFor="diagnostics-node">{t("diagnostics.field_node")}</FieldLabel>
            <Select items={readyNodes} value={nodeId} onValueChange={(value) => setNodeId(value ?? "")}>
              <SelectTrigger id="diagnostics-node"><SelectValue placeholder={t("diagnostics.field_node")} /></SelectTrigger>
              <SelectContent>{readyNodes.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
            </Select>
            {readyNodes.length === 0 && <FieldError>{t("diagnostics.no_ready_node")}</FieldError>}
          </Field>
          <Field>
            <FieldLabel htmlFor="diagnostics-target">{t("diagnostics.field_target")}</FieldLabel>
            <Select items={targetOptions} value={targetId} onValueChange={(value) => setTargetId(value ?? "")}>
              <SelectTrigger id="diagnostics-target"><SelectValue placeholder={t("diagnostics.field_target")} /></SelectTrigger>
              <SelectContent>{targetOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectContent>
            </Select>
            {targetOptions.length === 0 && <FieldError>{t("diagnostics.no_targets")}</FieldError>}
            <FieldDescription>{t("diagnostics.create_description")}</FieldDescription>
          </Field>
          <Field>
            <FieldLabel>{t("diagnostics.field_protocol")}</FieldLabel>
            <ToggleGroup variant="outline" spacing={0} value={[protocol]} onValueChange={(value) => value[0] && setProtocol(value[0] as DiagnosticProtocol)}>
              {protocols.map((option) => <ToggleGroupItem key={option} value={option} aria-label={option}>{option}</ToggleGroupItem>)}
            </ToggleGroup>
          </Field>
          {kind === "iperf3" && directions.length > 1 && (
            <Field>
              <FieldLabel>{t("diagnostics.field_direction")}</FieldLabel>
              <ToggleGroup variant="outline" spacing={0} value={[direction]} onValueChange={(value) => value[0] && setDirection(value[0] as DiagnosticDirection)}>
                {directions.map((option) => <ToggleGroupItem key={option} value={option} aria-label={option}>{option}</ToggleGroupItem>)}
              </ToggleGroup>
            </Field>
          )}
          {kind === "mtr" ? (
            <>
              <NumberField id="diagnostics-probes" label={t("diagnostics.field_probes")} value={values.probes} maximum={maxima.probes} onChange={(value) => setValues({ ...values, probes: value })} />
              <NumberField id="diagnostics-hop" label={t("diagnostics.field_hop_limit")} value={values.hopRange} maximum={maxima.hopRange} onChange={(value) => setValues({ ...values, hopRange: value })} />
            </>
          ) : (
            <>
              <NumberField id="diagnostics-duration" label={t("diagnostics.field_duration")} value={values.duration} maximum={maxima.duration} onChange={(value) => setValues({ ...values, duration: value })} invalid={durationInvalid} />
              <NumberField id="diagnostics-streams" label={t("diagnostics.field_streams")} value={values.streams} maximum={maxima.streams} onChange={(value) => setValues({ ...values, streams: value })} />
              <NumberField id="diagnostics-bitrate" label={t("diagnostics.field_bitrate")} value={values.bitrate} maximum={maxima.bitrate} onChange={(value) => setValues({ ...values, bitrate: value })} invalid={bitrateInvalid} />
              <Field orientation="horizontal">
                <Checkbox id="diagnostics-confirmation" checked={confirmed} onCheckedChange={(checked) => setConfirmed(checked === true)} />
                <FieldLabel htmlFor="diagnostics-confirmation">{t("diagnostics.field_confirmation")}</FieldLabel>
              </Field>
            </>
          )}
        </FieldGroup>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button disabled={submitDisabled} onClick={() => void submit()}>{submitting ? t("diagnostics.submitting") : t("diagnostics.submit")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function diagnosticDefaults() {
  return { probes: 10, hopRange: 32, duration: 10, streams: 1, bitrate: 100 }
}

function diagnosticMaxima(policy: DiagnosticPolicy | null, target: DiagnosticTarget | null) {
  const policyDuration = maximumOf(policy?.maximum_iperf_duration_seconds, 30)
  const policyBitrate = maximumOf(policy?.maximum_iperf_bitrate_mbps, 100)
  return {
    probes: 20,
    hopRange: 64,
    duration: Math.min(policyDuration, maximumOf(target?.maximum_duration_seconds, policyDuration)),
    streams: Math.min(4, maximumOf(target?.maximum_parallel_streams, 1)),
    bitrate: Math.min(policyBitrate, maximumOf(target?.maximum_bitrate_mbps, policyBitrate)),
  }
}

function NumberField(props: { id: string; label: string; value: number; maximum: number; invalid?: boolean; onChange: (value: number) => void }) {
  return (
    <Field data-invalid={props.invalid === true}>
      <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
      <Input id={props.id} type="number" min={1} max={props.maximum} value={props.value} aria-invalid={props.invalid === true}
        onChange={(event) => props.onChange(Number(event.target.value))} />
      <FieldDescription>{`1 – ${props.maximum}`}</FieldDescription>
      {props.invalid === true && <FieldError>{`1 – ${props.maximum}`}</FieldError>}
    </Field>
  )
}
