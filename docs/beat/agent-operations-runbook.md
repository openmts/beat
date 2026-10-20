# Agent operations

Remote operations require both an enabled owner policy on the Server and an
explicit local Agent configuration. Existing Agent configurations remain disabled.
The SSH terminal, metrics, custom collectors, and network reporting retain their
existing channels. Do not enable production policies before the complete v11
validation and deployment gates are satisfied.

## Local configuration

Add an `operations` object to the private Agent JSON configuration. Existing
server URL and per-node credentials remain in the normal enrollment configuration;
never copy a token into command arguments or operation content.

```json
{
  "operations": {
    "enabled": true,
    "state_dir": "/var/lib/beat-agent/operations",
    "max_concurrency": 1,
    "commands": {
      "enabled": true,
      "allow_ad_hoc": false,
      "run_as_user": "beat-ops",
      "allow_root": false,
      "working_root": "work",
      "maximum_timeout": "5m",
      "maximum_output_bytes": 262144,
      "cgroup_root": "/sys/fs/cgroup/beat-operations",
      "memory_bytes": 268435456,
      "maximum_processes": 64
    },
    "file_roots": [
      {
        "id": "work",
        "name": "Command workspace",
        "path": "/srv/beat-work",
        "maximum_file_bytes": 268435456
      },
      {
        "id": "inbox",
        "name": "Deployment inbox",
        "path": "/srv/beat-inbox",
        "read": false,
        "write": true,
        "maximum_file_bytes": 268435456,
        "overwrite": false,
        "allow_executable": false
      },
      {
        "id": "reports",
        "name": "Collected reports",
        "path": "/srv/beat-reports",
        "read": true,
        "write": false,
        "maximum_file_bytes": 268435456
      }
    ]
  }
}
```

Provision those exact directories before enabling operations. Roots must be
absolute, dedicated, private, and mutually non-overlapping. They cannot include
Agent configuration, operation state, the Agent executable, the configured shell,
or protected operating-system locations. Root names are optional human labels;
only IDs, names and limits are advertised, never local absolute paths.

On Linux, configuration/state/files use `0600` and directories `0700`. Transfer
roots and source files must belong to the Agent account; hard links, symlinks,
non-regular files and group/world-writable source files are rejected. The command
workspace is owned by the configured run-as account. Commands run through a
root controller that drops to the dedicated non-root account. Root execution
requires both the explicit local flag and the owner's policy. Rootless Agents
can monitor and transfer files, but cannot enable Linux command execution: using
the same unprivileged identity for controller and task cannot protect the journal
or cgroup from the task.

Linux commands require cgroup v2 with delegated `cpu`, `memory`, and `pids`
controllers. Provision an empty dedicated parent cgroup with those controllers
enabled; do not place unrelated processes in that parent. Delegation must be
enabled on both the root and the configured parent, otherwise every command task
fails closed with `sandbox_unavailable` (the Agent never falls back to
process-group-only control):

```sh
printf '+cpu +memory +pids' > /sys/fs/cgroup/cgroup.subtree_control
mkdir -p /sys/fs/cgroup/beat-operations
printf '+cpu +memory +pids' > /sys/fs/cgroup/beat-operations/cgroup.subtree_control
```

Run those two writes before starting the Agent and again after a reboot; on
systemd hosts the root-level write is reapplied by the boot, so a unit
`ExecStartPre` that rewrites the parent line keeps the delegation durable. If the
root write reports `device or resource busy`, move the Agent into its own slice
first. The Agent creates only its own attempt subgroups. A same-binary helper
sets resource limits, identity,
`no_new_privs` and parent-death handling before executing the configured shell.
A separate lifetime guardian kills the cgroup when the Agent disappears, including
descendants that create a new session. A missing containment facility fails closed.
There is no JSON switch that downgrades a deployment to process-group-only control.

## Windows

Use a dedicated Agent account and private service-account DACLs. Agent config and
state must retain medium-or-higher mandatory integrity; the process controller
must also have at least medium integrity. The optional local run-as name must
match that account. Linux shell tasks are not eligible; advertise and select
`powershell` for Windows.

Commands use a restricted, low-integrity token and a private PowerShell file,
with clean environment and the configured workspace as its temporary/profile
location. The workspace receives the service-specific restricting SID and a low
integrity label. Private Agent config/journals do not grant that SID; the generated
script grants it read access only. The child is created suspended, assigned to a
kill-on-close Job Object with process/memory/CPU limits, and resumed only after
its running journal is durable. Required API, token, DACL, or Job Object failures
reject execution. No alternate account or Administrator identity is selected by
Server input.

Windows directories must be on a local fixed volume with working DACL/integrity
support. File operations use handle-relative NT opens, reject reparse points and
volume changes, and publish with an atomic relative rename. A non-executable
transfer does not grant execute permission in its resulting service-account DACL.
The Windows Agent can be built from `cmd/agent`; runtime verification on an actual
Windows deployment is a separate acceptance gate, not established by cross-builds.

## File operations and recovery

Transfers handle one regular file within one configured root. The destination
parent must already exist. No remote directory listing, recursive copy, metadata
preservation, path escape, default overwrite, or implicit executable mode exists.
Linux opens require `openat2` with beneath/no-symlink/no-magic-link/no-cross-mount
resolution and a supported local filesystem. Windows uses the corresponding
handle/volume/reparse checks. Unsupported guarantees fail closed.

Chunks are exactly 4 MiB except for the final chunk; empty files have no chunks.
The client verifies content length, range, each SHA-256 and final size/digest.
Deploy uses a generated private temporary file in the destination parent. Its
committed chunk digests and publishing intent are synced before atomic publication.
After an interrupted publish, the Agent verifies the exact destination file ID,
mode and complete digest; it never blindly overwrites again. An uncertain outcome
is interrupted and requires operator reconciliation.

Collect pins an opened file and binds its size, digest, identity and version.
Resume reopens through the same protected root and revalidates that binding before
uploading missing chunks. Finalization is journaled and retried idempotently after
the Server has validated the complete artifact and any required scan. A changed
source or conflicting chunk fails the attempt. Temporary scan unavailability and
network failure retain the journal; timeout is measured from the original start
and is not reset by reconnect or restart.

Graceful Agent shutdown preserves resumable file-transfer progress. A canceled or
expired transfer cleans only its generated temporary file. A durable published or
finalized operation is reported as completed even if shutdown arrives afterward.
Commands use different restart semantics: authorized/running commands become
interrupted and are never relaunched automatically.

Completed results remain private until Server acknowledgement. Acknowledged
attempt IDs retain small content-free receipts, so a delayed duplicate lease
cannot execute the same attempt again after journal cleanup. The receipt count
has a hard limit; exhaustion fails closed. Do not erase receipts to resume work
under the same node identity. Reconcile/retire that identity and archive its state
before provisioning a replacement node and fresh state directory.

## Remove the service

`sudo beat-agent service uninstall` stops and disables `beat-agent.service` and
`beat-agent-update.service`, then deletes the unit files the installer wrote (only
files whose first line is the `# Managed by beat-agent` marker) together with
`/opt/beat-agent/beat-agent`, `beat-agent-update-helper` and the
`.beat-agent-update` state directory. A unit file without that marker, an unsafe
file mode, or a failing `systemctl disable` aborts the command before anything is
deleted, so a hand-written unit or a foreign binary is never removed silently.

The configuration and node identity under `/etc/beat-agent` are kept, so the same
machine can rejoin with `beat-agent join` or restore its service with
`beat-agent service install --config /etc/beat-agent/agent.json`. Add `--purge`
to delete `agent.json`, `enrollment.json`, `installation.json`, `identity.json`
and `discovery.json`; the directory itself is only removed when nothing else
remains in it. Purging destroys the node identity, so the machine must be enrolled
again (and, when it is being retired, removed from the Server) afterwards.

The command requires Linux, root and a running systemd, and fails with
`beat-agent service is not installed on this machine` when there is no
installation to remove.

## Observe and validate

Agent logs contain only attempt ID, state, reason, duration and byte counts.
Commands, output, paths, nonces and tokens are excluded. Subsystem errors use fixed
codes. Reporting and network runners continue while operations retry a runtime
transport failure. `Runner.Status` exposes local readiness and active count for
integrators; no operation state is published through metrics/MTS or public pages.

Before enabling a production policy, use one isolated node and private roots to
exercise durable-start response loss, duplicate lease/result delivery, cancellation,
timeout, output bound, Agent death with an escaped descendant, collect/deploy
resume, source mutation, path and mount races, existing destination conflicts,
clock/permission failures, and Server restore. Check both node login/monitoring
and the private operation audit. The complete batch must pass coverage, race,
format, lint, build, protocol and browser gates; a compiled implementation alone
is not acceptance evidence.
