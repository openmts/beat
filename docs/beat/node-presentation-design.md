# Node presentation controls

Status: Deployed and verified on 2026-07-30

## Goal

Bring Beat's node organization close to Komari and DStatus without changing
the public-first dashboard model. Administrators can control presentation while
agents continue reporting with the same identity and metrics contracts.

## Data contract

Each node stores platform data in SQLite:

- `sort_order`: non-negative order within its group.
- `tags`: a JSON array of at most 12 labels, each at most 32 Unicode code points.
- `is_public`: whether public node and network-quality views may expose the node.
- `public_remark`: operator-provided context visible on public cards and details.
- `private_remark`: administration-only notes, never serialized by public APIs.

Existing and newly registered nodes default to public visibility. Metrics remain
in MTS; presentation settings do not create time-series data in SQLite.

## Visibility boundary

Hidden nodes must be absent from every unauthenticated node surface:

- node list and node detail;
- node metric history;
- live metrics WebSocket snapshots;
- public network-quality task state and history.

Administration, alerting, terminal operations, Agent reporting and network task
execution continue to use all nodes. A hidden node is private, not disabled.

## Ordering and labels

Public and administrative node lists order by `sort_order`, then node name.
Administrators move nodes within one group; cross-group ordering requests are
rejected atomically. Tags are trimmed, deduplicated case-insensitively and keep
the first spelling supplied by the administrator.

## Interface

The existing edit dialog owns visibility, labels and remarks. Public cards show
a compact tag band and public remark. Administrative cards also show visibility
and private notes, with move-up and move-down actions in the existing action
menu. The layout stays dense enough for repeated operations but keeps remarks
and IP data bounded inside the card.

## Acceptance

1. Existing nodes remain visible after migration.
2. Hidden nodes return `404` from public detail and metric history and never
   appear in REST, WebSocket or public network-quality responses.
3. Private remarks appear only in authenticated managed-node responses.
4. Sorting is limited to one group and is transactionally applied.
5. Backend and frontend tests cover valid, boundary and invalid states.
