# GPU and accelerator telemetry

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and scope

Beat will add optional GPU telemetry without weakening the existing storage
boundary. SQLite stores device inventory, collection policy, and audit state.
Every utilization, memory, temperature, power, and error sample is written only
to MTS.

The first batch supports NVIDIA and AMD GPUs on Linux. Windows, macOS,
FreeBSD, Intel GPU, Apple accelerator, TPU, NPU, and vendor-specific counters
remain explicit capability states rather than guessed or zero-filled data.

This batch includes:

- stable multi-device inventory and lifecycle;
- per-device utilization, memory used/total, and temperature;
- optional power usage and power limit when the collector can prove units;
- current values on node cards and detailed MTS history;
- GPU alert metrics and durable notification routing;
- bounded collection, error visibility, backup, rollout, and rollback coverage.

It does not enable arbitrary commands, install vendor drivers, download vendor
tools, expose raw device serial numbers publicly, or store numeric samples in
SQLite.

## Competitor evidence

The inspected Komari Agent supports an opt-in detailed GPU report containing
device count, average utilization, and per-device name, memory, utilization,
and temperature. Its Server registers GPU metric definitions and tagged
per-device series; its Web UI exposes GPU inventory and chart series.

Reviewed sources:

- Komari Agent report:
  <https://github.com/komari-monitor/komari-agent/blob/e5aefd4f25f261e3b337671e1d71c1f4c4842b8f/monitoring/monitoring.go>
- Linux NVIDIA/AMD collection:
  <https://github.com/komari-monitor/komari-agent/blob/e5aefd4f25f261e3b337671e1d71c1f4c4842b8f/monitoring/unit/gpu_detailed_linux.go>
- Komari metric definitions:
  <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/internal/metricstore/definitions.go>
- Komari chart workbench in stable Web `1.3.2`:
  <https://github.com/komari-monitor/komari-web/blob/14f4067e9b69813b24a0255e565f7f49bff0a1bd/src/pages/instance/LoadChart.tsx>

The Web evidence is fixed to the stable release because the current Web `main`
was force-rewritten to an older unrelated history after the original review.

Beat adopts the useful telemetry outcome, not assumptions that a vendor binary
exists or that command output is trustworthy.

## Security and correctness invariants

1. GPU collection is disabled unless an administrator enables it for an Agent
   policy or installation.
2. The Agent executes only built-in, absolute-path collectors with fixed
   arguments. Server input cannot supply a command, path, environment variable,
   shell fragment, or device selector.
3. Collector execution has a context deadline, output byte limit, environment
   allowlist, and bounded device count.
4. A missing driver or collector reports `unsupported` or `unavailable`; it
   never emits synthetic zero samples.
5. Device identity uses a stable, non-secret fingerprint derived from vendor,
   PCI location or platform identity, and normalized model. Public responses use
   a node-local ordinal and display name, not a serial number.
6. All numeric samples are finite, unit-normalized, range-checked, and written
   only to MTS. SQLite never receives current or historical GPU values.
7. Unknown report fields are rejected. Duplicate device identities, duplicate
   metric samples, and more than the configured maximum devices reject the
   report rather than partially persisting it.
8. Inventory and MTS sample publication must not create a false active device:
   inventory changes are accepted only after the full report validates, and
   failed MTS writes make the report fail visibly.
9. GPU data follows node public visibility. Device serials, driver paths,
   collector stderr, and raw vendor output are administrator-only and redacted
   from normal API responses.

## Agent collection model

The Agent advertises a capability document with collector version, supported
vendors, available metrics, and a coarse failure code. The Server cannot turn
an unsupported capability into an enabled collector.

Linux collection priority is:

1. a native library or stable system API already approved as a dependency;
2. `/usr/bin/nvidia-smi` for NVIDIA;
3. `/opt/rocm/bin/rocm-smi` for AMD;
4. explicit unsupported state.

Command collectors run without a shell, with fixed locale, cleared proxy
variables, no inherited secrets, a five-second deadline, and a 256 KiB combined
output limit. The maximum accepted devices defaults to 32. Vendor values are
normalized to:

- utilization: percent, 0 through 100;
- memory: bytes, used not greater than total;
- temperature: degrees Celsius, within a defensive hardware range;
- power: watts, non-negative and bounded;
- count: integer derived from validated devices, not an independent sample.

One failed optional metric does not erase the remaining device, but its absence
is represented by an omitted field plus a bounded capability/error code. A
collector-wide parse or identity failure rejects the GPU section.

## Report and storage model

Accelerator telemetry uses canonical SQLite migration `v9` after the metric
catalog `v8`. It extends backup format `v4` through registered catalog entries
and does not introduce another archive envelope. The assignments are maintained
in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).

The Agent report gains a versioned optional `accelerators` object. Each device
contains a stable fingerprint, vendor, model, optional driver version, and only
the metrics advertised by its capability document.

SQLite adds inventory and policy tables only:

- `node_accelerators`: node ID, stable fingerprint hash, public ordinal,
  vendor, normalized model, capability bitmap, first/last observed time, and
  inactive time;
- `accelerator_collection_policies`: node or enrollment-policy scope, enabled
  state, sample interval, optional metric allowlist, and update metadata.

MTS measurements use the existing `node` tag plus bounded `device` and `vendor`
tags:

- `gpu_utilization_percent`;
- `gpu_memory_used_bytes`;
- `gpu_memory_total_bytes`;
- `gpu_temperature_celsius`;
- `gpu_power_watts` and `gpu_power_limit_watts` when supported.

`device` is the stable node-local fingerprint alias, not the display name.
Model, serial, driver version, PCI address, and arbitrary Agent labels never
become MTS tags. The metric catalog defines units, visibility, supported
aggregations, and retention; no code path dynamically creates an unbounded
measurement name.

Inventory mutation and MTS writes cannot share a transaction. The report path
therefore validates everything first, writes the MTS batch atomically, and then
upserts inventory in one SQLite immediate transaction. If inventory publication
fails after the MTS write, the report returns failure and a reconciliation job
replays the same idempotent inventory snapshot. MTS points use the report ID as
an idempotency key if the engine supports it; otherwise identical timestamp,
node, device, and measurement writes must be proven deterministic.

## API and frontend experience

Public node responses expose only active device ordinal, vendor, model,
capabilities, and latest permitted values. Administrator responses add
collection state and bounded failure codes. History queries use the shared
metric-catalog endpoint and MTS query path; there is no GPU-specific SQL
history API.

The public node card shows a compact `GPU` row only when a device exists. It
uses human-readable used/total memory and utilization, with an overflow-safe
device name. Multiple devices collapse to a count and highest-risk current
value; selecting the row opens node detail.

Node detail uses shadcn `Tabs`, `Card`, `Chart`, `Badge`, `Tooltip`, and
`Skeleton`. A device selector displays names while submitting stable IDs. The
history time control uses the same segmented presets and custom range as all
metrics. Changing the range must change the API `from` and `to`, the x-axis
domain, sampling interval, empty state, and accessible range summary.

GPU charts default to separate utilization, memory, and temperature panels.
Administrators may compose compatible series through the metric dashboard
design. Units are dynamically formatted from catalog metadata; byte and rate
series never share a y-axis with percent or temperature series.

## Alerts and notifications

Alert rules may target per-device utilization, memory percentage, temperature,
and power-limit percentage. Rules select a node plus either any GPU or a stable
device identity. Inventory rename or ordinal changes do not retarget a rule.

Trigger, resolve, notification message, and selected delivery rows follow the
durable notification transaction defined in the notification-delivery design.
A disappeared device resolves active value alerts with a `device_unavailable`
reason and may emit a separate inventory event if configured.

## Operations, backup, and rollback

Prometheus metrics cover collection outcomes, duration, device counts, rejected
reports, MTS write latency, inventory reconciliation, and stale GPU samples.
Labels use bounded vendor and reason enums. Readiness degrades only when the GPU
pipeline is enabled and its persistence or reconciliation worker is unhealthy;
a node without a GPU is healthy.

Current SQLite backups already include future inventory/policy tables, while
the logical MTS export must add every accelerator measurement and its bounded
tags. Restore validation rejects unknown units, invalid tags, and used values
greater than totals.

Rollback disables collection first, waits for in-flight reports, restores the
pre-migration Server/Agent and matching database backup, and leaves MTS GPU
measurements intact but unreachable. A later cleanup may remove them only after
explicit owner approval.

## Test and acceptance gates

Backend and Agent tests cover both vendors, missing tools, timeouts, oversized
output, malformed locale values, duplicate devices, stable identity, device
replacement, finite/range validation, tag bounds, atomic MTS batches, inventory
reconciliation, retry idempotency, alerts, backup/restore, and redaction.

Frontend tests cover zero/one/many devices, long model names, name-versus-ID
select behavior, responsive cards, dynamic units, incompatible series, custom
time ranges, x-axis domain changes, stale and partial metrics, keyboard access,
and loading/error/empty states.

Acceptance requires real NVIDIA and AMD evidence where available, a fixture
collector for CI, proof that SQLite contains no numeric GPU samples, proof that
all history survives restart in MTS, at least 90 percent coverage, race tests,
lint, production build, browser smoke tests, backup/restore drill, and IPv4/IPv6
deployment verification.

## Approval boundary

This design is canonical SQLite migration `v9` after metric catalog `v8`.
Implementation may begin only after the user explicitly approves this complete
design and any new dependency, schema, Agent protocol, release, or deployment
changes.
