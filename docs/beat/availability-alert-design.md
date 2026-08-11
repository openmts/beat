# Availability alerts

Status: Deployed and verified on 2026-07-30

## Goal

Beat must detect missing Agent heartbeats, show stale nodes as offline, create a
single active alert after a configurable debounce, and notify again when the
node resumes reporting. The feature reuses the existing alert rule, event and
channel pipeline instead of creating a parallel notification system.

## Rule contract

Availability rules use the metric `heartbeat_age_seconds`.

- `operator` is `gt`.
- `threshold` is the maximum accepted heartbeat age in seconds.
- `duration` is an additional debounce period before the event is created.
- Nodes that have never reported are skipped so provisioning does not create a
  false incident.
- Resource metric rules continue to evaluate online nodes only.

The server marks an online node offline after 90 seconds without a heartbeat.
This status transition is independent from whether an alert rule exists. The
next authenticated Agent heartbeat restores the node to online.

## Event lifecycle

Only one triggered event may exist for one rule and node. A healthy heartbeat
resolves that event even after a server restart, clears in-memory debounce
state, and sends a `resolved` payload through every enabled notification
channel. The stored event keeps its original trigger message and value; the
recovery payload uses a recovery-specific message and current heartbeat age.

## Interface

The alert rule dialog exposes "Node availability" as a metric. Selecting it
locks the operator to "greater than" and labels threshold and duration as
offline timeout and debounce. The events view uses authenticated managed nodes
so hidden nodes retain their names, and status labels remain localized.

## Acceptance

1. Stale online nodes become offline and fresh/offline nodes are unchanged.
2. A later heartbeat restores online status.
3. Availability rules skip never-seen nodes and honor threshold plus debounce.
4. One trigger and one recovery notification are delivered without duplicates.
5. Active events created before a restart resolve on the first healthy check.
6. Existing resource and traffic rules retain their behavior.
