# Scheduled traffic reports

## Goal

Beat sends completed-period traffic summaries through existing notification
channels. SQLite stores schedule configuration and execution state only. All
reported traffic values are aggregated from MTS at delivery time.

## Schedule contract

- Cadence: `daily`, `weekly`, or `monthly`.
- Time zone: an IANA time zone such as `Asia/Shanghai` or `UTC`.
- Delivery time: local hour and minute in the configured time zone.
- Weekly schedules select ISO weekday 1-7, Monday through Sunday.
- Monthly schedules select day 1-31; shorter months use their final day.
- Node scope and channel scope may target all records or explicit IDs.
- Disabled notification channels are never used by automatic runs.

Reports cover completed local periods:

- Daily: the previous local calendar day.
- Weekly: the seven local calendar days ending on the configured weekday.
- Monthly: the previous local calendar month.

These are operational transfer reports and always use immutable raw MTS deltas.
Billing-cycle calibration events described in
[`traffic-calibration-design.md`](./traffic-calibration-design.md) affect quota
summaries and alerts only; they never rewrite completed daily, weekly, or
calendar-month traffic facts.

## Persistence

`traffic_report_schedules` stores the schedule, `next_run_at`, the claimed
period key, the last run timestamp, and the last delivery result. Separate
join tables store explicit node and channel selections. Metric samples and
report totals are never copied into SQLite.

Before delivery, the scheduler atomically advances `next_run_at` and records
the period key only when the existing row is still due. This gives each period
at-most-once execution across restarts and concurrent scheduler loops. A crash
after claiming can lose that delivery, but cannot send it twice.

## Delivery

The scheduler builds one report from MTS and sends it to every enabled channel
in scope. Webhooks receive a typed `traffic_report` message; Telegram and email
receive the same human-readable summary. Existing alert webhook payloads are
unchanged.

The schedule records `success`, `partial`, or `failed`, plus the delivered
channel count and timestamp. Manual test runs use the latest completed period
but do not update `next_run_at`, the claimed period key, or the last automatic
run state.

## API and UI

All endpoints are admin-only under `/api/v1/alerts/traffic-reports` and expose
list, create, update, delete, and test-run operations. The admin alert page adds
a Traffic Reports tab with schedule, scope, next run, last run, and delivery
status. Node and channel scopes always render display names rather than IDs.
