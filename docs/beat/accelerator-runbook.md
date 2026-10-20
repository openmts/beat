# GPU / Accelerator telemetry

GPU telemetry uses SQLite v14 for inventory, policy and replay metadata; all GPU
readings and native sample receipts remain in MTS. Backups use the existing v4
envelope. The initial collectors support Linux NVIDIA and AMD.

Enable collection through a node policy or explicit installation configuration:

```json
"accelerators": {
  "enabled": true,
  "sample_interval_seconds": 30,
  "maximum_devices": 32
}
```

An absent server policy inherits installation opt-in. An explicitly disabled
node policy overrides installation settings. The default enrollment policy is
copied once to newly enrolled nodes and preserves any existing node policy.
Policy writes require recent authentication; enrollment defaults are owner-only.
GPU admission uses active per-node credentials; legacy disabled capability
reports preserve ordinary host telemetry.

The collectors use installed `/usr/bin/nvidia-smi` and
`/opt/rocm/bin/rocm-smi` with fixed arguments, a clean environment, a five-second
collection deadline and a 256 KiB combined stdout/stderr limit per command.
Intervals are 5–3600 seconds, with at most 32 devices per snapshot and 256
historical identities per node. Missing tools, failed collection and optional
missing fields produce bounded states/codes instead of synthetic zeros.

Public devices expose a node-local `device_id`, ordinal, vendor, model,
capabilities and permitted MTS values. Histories and dashboard series must bind
both `node_id` and `device_id`; ordinals and node variables cannot retarget GPU
history. Fingerprint hashes and driver versions are administration metadata.

GPU capture time and cadence are independent of host heartbeats. Retried reports
keep their original body; a policy-conflict ACK drops that pending report and
allows a new host sample. Stale means native sample age exceeds
`max(3 × cadence, 30 seconds)`.

MTS writes GPU values, the native accelerator receipt and the host receipt in one
batch. Only then may SQLite publish inventory. A bounded reconciliation worker
replays metadata after a failed publication. A failed vendor enumeration retains
its prior inventory; a successful complete enumeration marks missing devices
inactive. Readiness includes first-installation pending work when enabled.

A rollback first disables node/default enrollment policies and drains in-flight
reports, then restores matching Server/Agent binaries and the pre-migration
backup. Existing MTS GPU data remains subject to its catalog retention policy.
Native receipts retain at least the longest GPU metric retention period.

Automated verification uses synthetic vendor output and private fixture
executables. Real NVIDIA/AMD driver and hardware acceptance remains an operator
verification step for the deployment environment.
