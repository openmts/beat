import type {
  TrafficReportCadence,
  TrafficReportSchedule,
  TrafficReportSchedulePayload,
} from "@/types"

export interface TrafficReportForm {
  name: string
  cadence: TrafficReportCadence
  timezone: string
  sendHour: string
  sendMinute: string
  weekday: string
  monthDay: string
  allNodes: boolean
  nodeIds: string[]
  allChannels: boolean
  channelIds: string[]
  enabled: boolean
}

export function emptyTrafficReportForm(): TrafficReportForm {
  return {
    name: "",
    cadence: "daily",
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    sendHour: "8",
    sendMinute: "0",
    weekday: "1",
    monthDay: "1",
    allNodes: true,
    nodeIds: [],
    allChannels: true,
    channelIds: [],
    enabled: true,
  }
}

export function trafficReportFormFrom(schedule: TrafficReportSchedule): TrafficReportForm {
  return {
    name: schedule.name,
    cadence: schedule.cadence,
    timezone: schedule.timezone,
    sendHour: String(schedule.send_hour),
    sendMinute: String(schedule.send_minute),
    weekday: String(schedule.weekday),
    monthDay: String(schedule.month_day),
    allNodes: schedule.all_nodes,
    nodeIds: schedule.node_ids,
    allChannels: schedule.all_channels,
    channelIds: schedule.channel_ids,
    enabled: schedule.enabled,
  }
}

export function trafficReportPayload(form: TrafficReportForm): TrafficReportSchedulePayload {
  return {
    name: form.name.trim(),
    cadence: form.cadence,
    timezone: form.timezone.trim(),
    send_hour: Number(form.sendHour),
    send_minute: Number(form.sendMinute),
    weekday: Number(form.weekday),
    month_day: Number(form.monthDay),
    all_nodes: form.allNodes,
    node_ids: form.allNodes ? [] : form.nodeIds,
    all_channels: form.allChannels,
    channel_ids: form.allChannels ? [] : form.channelIds,
    enabled: form.enabled,
  }
}

export function trafficReportFormError(form: TrafficReportForm): string | null {
  if (!form.name.trim()) return "traffic_report.validation_name"
  if (!form.timezone.trim()) return "traffic_report.validation_timezone"
  if (!wholeNumberInRange(form.sendHour, 0, 23) || !wholeNumberInRange(form.sendMinute, 0, 59)) {
    return "traffic_report.validation_time"
  }
  if (form.cadence === "weekly" && !wholeNumberInRange(form.weekday, 1, 7)) {
    return "traffic_report.validation_weekday"
  }
  if (form.cadence === "monthly" && !wholeNumberInRange(form.monthDay, 1, 31)) {
    return "traffic_report.validation_month_day"
  }
  if (!form.allNodes && form.nodeIds.length === 0) return "traffic_report.validation_nodes"
  if (!form.allChannels && form.channelIds.length === 0) return "traffic_report.validation_channels"
  return null
}

export function toggleTrafficReportTarget(values: string[], id: string, checked: boolean): string[] {
  if (checked) return values.includes(id) ? values : [...values, id]
  return values.filter((value) => value !== id)
}

function wholeNumberInRange(value: string, minimum: number, maximum: number): boolean {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum
}
