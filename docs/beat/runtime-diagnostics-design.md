# Runtime diagnostics

Updated: 2026-07-30

Status: reviewed design; implementation requires explicit approval

## Goal and competitor evidence

Komari exposes administrator runtime summaries and downloadable Go CPU, trace,
heap, goroutine, mutex, allocation, block, and thread profiles. Beat currently
exports bounded Prometheus process metrics but cannot capture evidence for a
production CPU, allocation, lock, or goroutine incident.

Evidence:

- <https://github.com/komari-monitor/komari/blob/4077201f098774511eaf504f220c5f6be009346b/web/api/admin/pprof.go>
- <https://github.com/komari-monitor/komari-web/blob/14f4067e9b69813b24a0255e565f7f49bff0a1bd/src/pages/admin/pprof.tsx>

Beat will provide bounded authenticated captures without registering the
standard public `/debug/pprof` mux.

## Security invariants

1. Summary and capture routes require an owner session. Starting, downloading,
   or deleting a capture also requires recent reauthentication.
2. No public, Agent, service-token, theme, wallboard, or ordinary administrator
   principal can access runtime profiles.
3. Capture type, duration, debug level, sample rate, output size, concurrency,
   expiry, and total disk usage are fixed server-side bounds. The request cannot
   pass an arbitrary profile name or runtime knob.
4. Exactly one CPU/trace capture and one instantaneous profile capture may run
   globally. Queueing is not supported; contention returns a conflict.
5. Profiles are sensitive artifacts. They use a dedicated `0700` directory and
   `0600` random files, are excluded from backup, never enter SQLite or MTS, and
   expire after 15 minutes or one successful download.
6. Restart deletes every orphan diagnostic file before serving. A failed capture
   closes and removes only its generated file.
7. Audit, logs, metrics, filenames, and errors never contain profile bytes,
   stack text, request headers, cookies, tokens, environment values, or absolute
   private paths.
8. Diagnostics cannot change runtime profiling rates globally after capture,
   trigger garbage collection repeatedly, block readiness, or restart the
   process.

## Capture contract

Supported capture kinds are:

- `cpu`: 5 to 30 seconds;
- `trace`: 1 to 5 seconds;
- `heap`, `allocs`, `goroutine`, `mutex`, `block`, and `threadcreate`:
  instantaneous binary profiles with fixed debug level zero;
- `summary`: a JSON snapshot of build version, uptime, Go version, process
  memory classes, goroutine/thread counts, GC totals, scheduler state, storage
  readiness, and capture availability.

The summary omits environment variables, command arguments, filesystem paths,
network peers, node names, account data, database names, query text, and labels
with unbounded values. Numeric runtime history is not persisted as an alternate
time-series backend; existing Prometheus metrics remain the only ongoing
runtime signal.

Capture creation returns a random opaque ID, kind, start/deadline, and status.
The in-memory registry holds owner ID, session ID, timestamps, byte count, and
fixed failure code. Completed bytes live only in the private file. Restart loses
the registry and removes files; no SQLite migration is needed.

## API and authorization

```text
GET    /api/v1/admin/runtime/summary
POST   /api/v1/admin/runtime/captures
GET    /api/v1/admin/runtime/captures
GET    /api/v1/admin/runtime/captures/{id}/download
DELETE /api/v1/admin/runtime/captures/{id}
```

Creation accepts only `{kind, duration_seconds}`. Listing returns the current
owner's capture metadata and global busy state, never another owner's artifact.
Download sets `Content-Disposition: attachment`, `Content-Type:
application/octet-stream`, `Cache-Control: no-store`, `X-Content-Type-Options:
nosniff`, and an exact bounded length. A completed download atomically consumes
the artifact. Range requests and public caching are unsupported in version one.

Cancellation deletes a pending/complete artifact. CPU and trace cancellation
stops capture context first and waits for the runtime writer to close before
unlinking. Logout or session revocation cancels that session's active captures.

## Administration experience

Add an owner-only `Runtime diagnostics` tab to the compact operations surface.
It is a quiet work tool, not a dashboard hero.

- A single summary band shows build, uptime, Go runtime, memory, goroutines, GC,
  storage/readiness, and current capture state.
- A `FieldGroup` contains capture kind `Select`, duration stepper for CPU/trace,
  and one `Start capture` button with an `ActivityIcon`.
- An `Alert` states that profiles may contain sensitive operational metadata and
  expire quickly.
- A flat `Table` shows kind, owner, started, status `Badge`, size, expiry, and
  icon actions for download/cancel/delete. Empty and loading states use shadcn
  `Empty` and `Skeleton` patterns.
- Starting CPU/trace opens an `AlertDialog` naming duration and impact. The
  confirmation action remains `Start capture` through toast and audit text.
- Progress updates through bounded polling without page reload. Leaving the tab
  does not cancel a confirmed capture.

Desktop uses one constrained column with no nested cards. Mobile turns table
rows into flat capture cards with stable action space. IDs and filesystem paths
are never visible; long build values truncate with a tooltip. Status always uses
icon plus text and keyboard focus remains visible.

## Limits, failure, and observability

Default limits are 30 seconds CPU, 5 seconds trace, 64 MiB per artifact, 128 MiB
total diagnostics disk, one active timed capture, two instantaneous captures per
minute per owner, and 15-minute expiry. Limit constants are tested and may be
lowered by deployment config, never raised without a reviewed release change.

Prometheus exposes bounded counters/gauges for captures by fixed kind/result,
active captures, bytes, cleanup failures, and rejected limits. Audit actions are
`runtime.capture.start`, `.complete`, `.cancel`, `.download`, `.expire`, and
`.fail`, containing fixed kind, duration, size, request ID, and no profile data.

Diagnostic subsystem failure appears in authenticated summary and metrics but
does not make the monitoring service globally unready. A leaked/uncleanable
artifact raises a high-severity operational log and disables new captures until
cleanup succeeds.

## Tests and acceptance

Tests cover every role/reauth/session boundary, ID ownership, CSRF/origin rule,
kind/duration/body limits, one-at-a-time conflicts, rate limiting, cancellation,
logout revocation, disconnect, timeout, size overflow, disk full, short writes,
runtime writer errors, startup orphan cleanup, expiry, one-time download, cache
headers, and no public/router shadowing.

Leak tests prove profile bytes, stack output, paths, cookies, tokens, headers,
environment, and node/account values do not enter logs, audit, metrics, SQLite,
MTS, backups, or frontend state. Frontend tests cover shadcn composition,
human-readable states, confirmation, progress, error recovery, responsive
overflow, focus, and no full-page refresh.

Completion requires at least 90 percent backend statement and frontend line
coverage, race/shuffle, goroutine leak checks, `go vet`, `goimports-reviser`,
`golangci-lint`, vulnerability/module verification, production build, browser
smoke, rootless container writable-path proof, and deployed IPv4/IPv6 admin and
public-isolation acceptance.

## Version and approval boundary

This is schema-free release batch `runtime-diagnostics` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
It depends on deployed account security and may ship independently of Agent
diagnostics migration `v14`. It changes no backup format and creates no metric
history outside MTS.

Implementation requires separate explicit approval because runtime profiles are
sensitive, CPU/trace capture can affect a production process, and the batch adds
new owner-only routes plus a private writable artifact directory.
