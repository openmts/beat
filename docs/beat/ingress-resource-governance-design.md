# Ingress resource governance

Updated: 2026-07-30

Status: reviewed design; owner approved implementation on 2026-10-15 (blanket approval of all pending items); the admission-controller batch is implemented in the current worktree (see Implementation notes)

## Goal and current evidence

Beat must remain available when anonymous browsers, authenticated Agents, or
administrators create concurrent or oversized work. Existing controls are
useful but route-specific: the Server has read/write timeouts, login failures
eventually lock an IP and username, authentication bodies are limited to 16
KiB, network-result bodies to 256 KiB, and backup uploads have their own archive
limit. They do not form a complete admission or cost policy.

The confirmed gaps are:

- concurrent login requests pass `Allowed` before any failure is recorded, so
  one source can start many Argon2id verifications at approximately 64 MiB each;
- public node history accepts an arbitrary comma-separated metric count and any
  RFC3339 range, then runs one unbounded MTS query and retains every point for
  each metric;
- public Fleet list/detail and network-quality summary rebuild MTS-derived data
  for every request without shared cache, singleflight, rate, or concurrency
  limits;
- a valid Agent token can submit unbounded node-report JSON and repeat reports
  without a per-node or global ingestion budget;
- most management JSON uses an unbounded decoder, accepts trailing objects and
  does not consistently validate request content type;
- node-detail and network polling can overlap, continue while the document is
  hidden, and do not cancel obsolete requests;
- common JSON response helpers deliberately ignore encoder/write errors.

This schema-free batch supplies one enforceable capacity model while preserving
public no-login reads, authenticated administration, and MTS-only time series.

## Security and storage invariants

1. Public monitoring remains available without login. Admission control never
   becomes an authentication mechanism or a private-site gate.
2. Administration remains session/authorization protected. A trusted proxy or
   allowed Origin does not grant more budget or permission by itself.
3. Agent, probe, derived, and other numeric samples remain exclusively in MTS.
   SQLite stores no request counters, rate-limit samples, query points, or
   transient admission state.
4. Client identity uses the deployed canonical trusted-proxy resolver. Forged
   forwarding headers cannot create new limit keys.
5. Raw IPs, usernames, node IDs, session IDs, paths, queries, Origins, and error
   strings never become Prometheus labels.
6. Limits reject work before expensive password, SQLite, MTS, notification, or
   SSH operations. Rejection does not enqueue unbounded delayed work.
7. This batch consumes no SQLite migration and keeps backup format `v4`.
8. WebSocket connection/fanout budgets remain owned by
   [`runtime-resilience-design.md`](./runtime-resilience-design.md); this batch
   owns HTTP, authentication, history-query, and Agent-ingestion admission.

## Capacity configuration

The Server constructs one immutable, validated capacity configuration before
opening listeners. Code-owned safe defaults are published in the runbook; a
small bounded set of environment overrides supports measured deployment sizes.
Startup rejects zero, negative, overflowing, internally inconsistent, or
unsafe-high values instead of silently disabling protection.

The initial envelope includes:

- explicit `MaxHeaderBytes` of 32 KiB and request-target maximum of 8 KiB;
- 16 KiB authentication JSON, 64 KiB ordinary/admin JSON, and 64 KiB node
  reports;
- the existing 256 KiB network-result limit and independent backup/diagnostic
  limits;
- global and per-identity token-bucket rates for route classes;
- global and per-identity in-flight limits for expensive route classes;
- maximum history series, duration, points per series, scanned-cost score, and
  encoded response bytes;
- zero admission wait for every ingress class; Agents retry with bounded jitter
  instead of occupying request goroutines in an internal queue.

The authenticated Operations status exposes effective numeric limits and
saturation state without listing client identities. Environment changes require
restart; the browser cannot edit them.

## Admission controller

Routes declare a fixed class rather than constructing ad hoc limiters:

- `public_static`: site/group metadata and static shell;
- `public_snapshot`: Fleet/node/network-quality current views;
- `public_history`: metric and network history;
- `auth_kdf`: login, bootstrap password creation, reauthentication, and
  password change;
- `agent_read`: assignment fetches;
- `agent_write`: node reports and network-result batches;
- `admin_read`, `admin_write`, and `admin_expensive`;
- upload/diagnostic classes owned by their feature designs.

Admission is layered: one global bucket, one global in-flight semaphore, then a
canonical identity bucket and in-flight lease. Identity is client IP for public
and pre-auth requests, authenticated node ID for Agent work, and administrator
user/session for protected work. Agent routes retain a coarse per-IP ceiling so
one compromised NAT/source cannot consume the whole global budget, while the
per-node key avoids penalizing normal fleets behind NAT.

Limiter state is bounded and TTL-based. Active lockouts or leases are never
arbitrarily evicted to make room for attacker-created keys. Expired entries are
removed first; if the table is full, unseen identities fail closed under the
global class limit until capacity returns. All lease releases are deferred at
the request boundary and execute on success, error, panic, or cancellation.

Rejected rate work returns `429` with a bounded integer `Retry-After`; global
dependency saturation returns `503`. Oversized body/request target and invalid
media return `413`, `414`, or `415`. Responses use one generic message and do
not disclose bucket capacity, account existence, node existence, or global
load. CORS and same-origin behavior remain route-specific and unchanged.

## Authentication work factor protection

Every password-hash operation acquires the `auth_kdf` admission lease before
database lookup or Argon2 work. The default allows one in-flight KDF per client
IP and a small global count derived from the documented memory envelope. A
burst of concurrent requests from one IP therefore cannot reserve multiples of
the 64 MiB work factor before failure accounting catches up.

Login uses global, IP, and normalized-username failure keys. Failure reservation
and release are atomic with the in-flight lease. A success clears only the
applicable identity state. A full limiter table never deletes an active victim
lockout. Unknown, disabled, wrong-password, and wrong-TOTP outcomes retain
constant password work and uniform public errors.

Reauthentication and password changes use the same KDF gate plus per-session
and per-user failure limits. Bootstrap has a separate low-rate class, remains
same-origin, verifies the bootstrap authority before hashing, and is disabled
after setup. The durable threshold/security-event behavior from migration
`v17` composes with these in-memory availability controls.

Password hash parsing also enforces supported upper bounds for memory,
iterations, parallelism, salt, and output length before allocation. A corrupt or
restored hash cannot request unbounded Argon2 resources. Unsupported parameters
fail closed and surface an owner recovery condition without echoing the hash.

## Request body and JSON contract

Body limits are attached to routes before authentication or decoding, with a
smaller resolver limit retained for legacy Agent migration. JSON endpoints
require `application/json` with an optional UTF-8 charset, decode exactly one
value, reject trailing non-whitespace, and return stable errors. Management and
authentication payloads reject unknown fields; versioned Agent protocols may
allow explicitly documented forward fields only within the body limit.

Field-level validation remains mandatory after decoding. Node report inventory
strings gain byte/rune limits; sort arrays, target lists, tags, URLs, commands,
notification configuration, and every repeated field have count and element
limits. A body cap is not a substitute for semantic validation.

Handlers use one bounded decode helper that distinguishes malformed JSON,
oversize, unsupported media, cancellation, and internal failure without
returning decoder internals. Tests enumerate every write route so a new handler
cannot bypass a declared body policy.

## Public snapshot and history budgets

`public_snapshot` routes do not independently rebuild identical current data.
Fleet list/detail reads consume the immutable last-good snapshot created by the
runtime-resilience producer, apply server-side visibility before filtering, and
reuse its encoded generation/ETag. Network-quality current views use a bounded
singleflight cache with one refresh owner, fixed freshness, stale annotation,
and no request-triggered fanout of concurrent MTS work.

Cache keys include visibility, effective public settings, route parameters, and
generation. Private/admin content never shares a public cache entry. Conditional
GET returns `304` without touching MTS. Cache failure keeps a bounded last-good
response only within its stale window and never labels it current.

Until metric catalog `v8` generalizes the query model, node history accepts only
one to eight known public metrics, `from < to`, a maximum 31-day range, and a
fixed maximum of 600 returned points per series. Unknown/duplicate metrics,
future-only ranges, excessive request targets, and incompatible combinations
return `400` before MTS access.

The Server selects deterministic metric-specific aggregation and a bucket width
from the requested range so MTS returns at most the point budget; fetching all
raw rows and downsampling in Go is forbidden. A global MTS-query semaphore,
per-IP history lease, deadline, scanned-cost ceiling, and encoded response cap
apply to both current and future catalog endpoints. The `v8` contract later
adds catalog units/tags/aggregations without weakening these ingress controls.

Network history retains its existing 31-day and 600-point contract but enters
the same admission class. Public responses never reveal private nodes/tasks and
do not accept arbitrary tag expressions or measurement names.

## Agent ingestion governance

Agent authentication still resolves one concrete node before any write. The
node-report body is capped at 64 KiB, inventory strings are bounded, metrics are
finite/non-negative and root-filesystem-only, and the authenticated node ID is
the only MTS tag identity.

Each node has at most one active report write and a token-bucket rate compatible
with the supported minimum report interval. A global Agent-write semaphore
protects SQLite/MTS, while a coarse source ceiling contains a compromised
network. Excess reports are rejected before heartbeat or MTS mutation; they are
not buffered in memory. Accepted work has a context deadline and releases every
lease on cancellation.

Per-node serialization preserves traffic-delta ordering. The node-lock registry
has lifecycle cleanup when a node is tombstoned/deleted so node churn cannot
grow it forever. Network-result batches retain their 64-result/body bounds and
also receive per-node/global admission. Duplicate or replay semantics remain
owned by the Agent protocol and metric-erasure designs, not by client IP.

## Response and frontend behavior

Small JSON responses encode into a bounded buffer before headers are committed.
Shared snapshots reuse pre-encoded immutable bytes. Bounded history responses
either encode successfully or return a generic error; response write/flush
errors are always captured, classified, and observed. Handlers never ignore an
encoder error and never retry a partial response.

Frontend polling for node history and network views is visibility-aware, keeps
one request in flight, cancels obsolete generations with `AbortController`, and
uses capped jittered retry. A range/node change aborts the prior request. `429`
honors bounded `Retry-After`; `503` uses normal backoff. Existing data remains
visible with a stale/error indicator instead of being blanked or repeatedly
re-requested. Public pages never respond to rejection by attempting login.

## Observability and operations

Prometheus adds bounded series for admitted/rejected requests, in-flight work,
limiter entries, KDF work, body/target rejection, snapshot cache outcomes,
history query duration/scanned/returned points, response bytes/errors, and
Agent report outcomes. Labels are fixed class/result/reason enums only.

Transient client overload does not fail readiness and must not cause a load
balancer to remove every instance. Readiness fails only if admission machinery
is uninitialized/corrupt or required snapshot/query dependencies are unhealthy.
Sustained global saturation, rejection ratio, KDF queue pressure, MTS query
deadline, and Agent rejection alerts are documented in the runbook.

The authenticated Operations view adds a read-only `Ingress` tab using shadcn
`Table`, `Badge`, `Progress`, and `Alert`. It shows fixed route classes,
effective limits, current/global utilization, rejection rate, and cache state.
It exposes no identities, arbitrary reset controls, IP allowlists, or remote
tuning form.

## Tests and acceptance

Backend tests cover route-policy enumeration, header/target/media/body/trailing
limits, unknown fields, every release path, proxy identity, TTL/full-table
behavior, deterministic no-eviction lockouts, and generic error responses.

Authentication tests launch synchronized same-IP and distributed bursts and
prove KDF concurrency/memory work never exceeds its gate, user enumeration does
not change responses, reauthentication is bounded, stored hash parameters are
upper-bounded, and cancellation releases leases.

Public tests prove one shared snapshot/cache build serves many callers, ETags
avoid MTS work, private visibility cannot cross cache keys, one to eight known
metrics and 31 days/600 points are enforced, aggregation occurs in MTS, cost and
response limits reject before excessive allocation, and slow/disconnected
writers are observed.

Agent tests cover 64 KiB reports, inventory limits, per-node/global/source
bursts, one active write per node, no mutation after rejection, traffic ordering,
deadline/cancellation, lock cleanup, many-node NAT behavior, and MTS-only values.
Frontend tests cover one in-flight poll, abort on range/node change, hidden-tab
pause/resume, `Retry-After`, backoff, preserved stale data, and no login redirect.

Load/soak acceptance measures anonymous snapshot/history traffic, synchronized
login bursts, expected Agent fleets, slow clients, MTS latency, memory ceiling,
and recovery after saturation over IPv4 and IPv6. Completion also requires 90
percent coverage, race/shuffle, `go vet`, `goimports-reviser`, `golangci-lint`,
vulnerability/module verification, frontend audit/lint/build, and browser smoke.

## Delivery, rollback, and approval

This is schema-free batch `ingress-governance` in
[`commercial-parity-implementation-sequence.md`](./commercial-parity-implementation-sequence.md).
Pre-mutation Agent admission composes with, but does not define, stable sample
identity, retry acknowledgement, traffic-delta idempotency, or telemetry
freshness. Those semantics are owned by
[`agent-ingest-consistency-design.md`](./agent-ingest-consistency-design.md).
It depends on deployed trusted-proxy identity, composes with
`runtime-resilience`, and is generalized by metric catalog `v8` and durable
login-security migration `v17`. It consumes no SQLite migration, leaves the
next migration at `v25`, and keeps backup format `v4`.

Privileged route registration must also compose one admission class with one
explicit audit policy from
[`administrative-mutation-consistency-design.md`](./administrative-mutation-consistency-design.md).
Admission rejection happens before mutation or external admission and uses the
same request context, but it does not fabricate a domain success/failure audit
event or permit a generic route-policy fallback.

Rollback restores the previous binary/frontend and creates no persistent data
conversion. It also removes the new rejection/status metrics, so rollback
alerts must be version-aware.

Implementation requires explicit approval because it changes accepted request
sizes/media, public history validation/aggregation, overload status codes,
authentication and Agent admission, response headers, polling lifecycle, Server
configuration, and operations output. This design alone authorizes none of
those changes.

## Implementation notes, 2026-10-15

按「批准所有待审批项」推进 `ingress-governance`，本批（schema-free，不占用迁移号、不改备份格式
`v4`）到位的部分：

- `internal/ingress` 提供与路由无关的准入核心：9 个固定类别（`public_static`、
  `public_snapshot`、`public_history`、`auth_kdf`、`agent_read`、`agent_write`、`admin_read`、
  `admin_write`、`admin_expensive`）、启动期校验的不可变容量配置、全局与身份令牌桶、全局与
  身份在途租约，以及有界 TTL 身份表；
- `Classify(method, path)` 用一张显式规则表把请求映射到类别：登录/引导/重认证/改密进入
  `auth_kdf`，节点上报与网络结果进入 `agent_write`（认证后还有一次按节点 ID 的取号），
  公共历史进入 `public_history`，上传/导出/下载/抓取进入 `admin_expensive`，Agent WebSocket
  明确不进入准入（其预算属于 `runtime-resilience`）；
- `Capacity` 默认值带代码自带的安全默认，环境变量只允许有界覆盖
  （`BEAT_INGRESS_<CLASS>_{GLOBAL_RATE,GLOBAL_BURST,IDENTITY_RATE,IDENTITY_BURST,GLOBAL_INFLIGHT,IDENTITY_INFLIGHT}`、
  `BEAT_INGRESS_IDENTITY_ENTRIES`、`BEAT_INGRESS_IDENTITY_TTL`）；零值、负值、越界与内部不一致
  的取值在启动期直接失败，不做静默降级；
- 拒绝语义固定：速率类返回 `429` 且有界 `Retry-After`，全局在途饱和返回 `503`，身份表满且
  出现新身份时在全局类别预算下失败关闭；响应只有一条通用消息，不泄露桶容量、账号或节点是否
  存在；原始 IP、会话令牌与节点 ID 都不进入指标标签（会话与节点标识先做不可逆指纹）；
- `internal/api/middleware.Admission` 在进入处理器之前取号，租约在请求边界释放，成功、失败、
  panic 与取消都会归还；`internal/api/routes_agent_report.go` 在上报认证之后、任何心跳或 MTS
  写入之前按认证节点 ID 再取一次号，因此每个节点只保留一次在途上报，同时外层保留来源封顶；
- `beat_ingress_admitted_total{class}`、`beat_ingress_rejected_total{class,reason}`、
  `beat_ingress_in_flight{class}`、`beat_ingress_identity_entries{class}` 及其上限仪表暴露同一份
  只读快照；`GET /api/v1/admin/runtime`（仅 owner）新增只读 `ingress` 区块，Remote operations
  页面新增「入口准入」页签，只有类别、有效上限、当前占用与拒绝计数，没有身份、没有重置按钮，
  浏览器不能改配额。

公共指标工作区的匿名只读接口不再占用管理面额度：`GET /api/v1/metrics/catalog`、
`GET /api/v1/metrics/dashboards/default`、`GET /api/v1/metrics/annotations`、
`GET /api/v1/metrics/snapshots/{token}` 进入 `public_static`，`POST /api/v1/metrics/query`
进入 `public_history`（POST 只是查询体承载方式，语义仍是匿名读）。`/api/v1/admin/metrics/*`
保持 `admin_read` / `admin_write` 不变，公共页面与后台页面因此不再共享同一个令牌桶。

WebSocket 升级入口不属于任何 HTTP 令牌桶（不变式 8）：`GET /api/v1/ws/metrics` 与
`GET /api/v1/agents/ws` 一样在 `Classify` 里声明为不受准入控制，由 `internal/metricsfanout`
自己的握手速率（每 IP 1/s、突发 8，全局 32/s、突发 64）、每 IP 16 条连接、全局 1024 条连接
与在途租约负责。长连接持续数分钟到数小时，若计入 HTTP 在途额度，会把有限的在途名额长期
占满并连带阻塞同一身份的正常读请求。

证据：`internal/ingress` 定向用例覆盖类别映射、容量校验与环境覆盖、速率/在途/表满拒绝、
租约幂等释放与统计快照；`internal/api/middleware` 新用例覆盖拒绝响应、跳过未声明路径、身份
解析与指纹；`go build ./...`、`goimports-reviser`、`golangci-lint`（0 issues）与前端
`tsc`、`vitest`、`npm run build` 干净。

第二批落地公共快照复用（仍为 schema-free）：

- `MetricsHandler` 在每次成功的指标快照构建后，把不可变快照与预编码字节、`generation`、
  ETag 一起放进 `atomic.Pointer`；节点列表与节点详情通过 `publicSnapshotSource` 读同一份
  快照，共享同一次构建的编码结果，而不是各自重建；
- 复用窗口是 `metricsfanout.Cadence + 1s`（当前 6s）；窗口内命中直接返回共享字节并复用
  `ETag`，`If-None-Match` 命中返回 `304` 且不触碰 MTS；
- 快照里没有的节点 ID 返回 `404`，不回落到直连，避免“快照缺节点”被误判为“节点不存在于
  服务端”以外的语义；快照超过复用窗口或尚未构建时才回退直连查询；
- `TelemetryAgeSeconds` 等时间相对字段在每次请求时按当前时间重算，且只修改值拷贝，不写回
  共享快照，因此复用不会让不同请求读到彼此的时间基准；
- 分组过滤与公开可见性过滤都作用在快照副本上，私有/管理内容不进入公共快照。

证据：`internal/api/handler/public_snapshot_test.go` 覆盖列表/详情复用、分组过滤、`304`、
快照缺节点、过期回退与年龄重算；`internal/model`、`internal/store` 看板与擦除定向用例、
`internal/api` 路由用例、`go build ./...`、`go vet`、`goimports-reviser`、
`golangci-lint`（0 issues）干净。

第三批落地统一的有界 JSON 解码契约（仍为 schema-free）：

- `internal/api/handler/body.go` 是唯一解码入口：先在预算内读取（默认 1 MiB，认证面使用更小
  的既有上限），再校验媒体类型，最后只接受恰好一个 JSON 值；`BodyError` 把失败归类为
  `malformed`/`oversize`/`media_type`/`cancelled`/`trailing`/`internal`，对外只暴露稳定状态码与
  固定文案（`400`/`413`/`415`/`408`），不返回解码器内部信息；
- 空请求体保留原有语义：调用方决定是否容忍 `io.EOF`（例如 `DELETE` 携带可选
  `expected_revision`）；只有非空请求体才要求 `application/json`（可带 charset 参数）；
- 管理面与认证面载荷使用严格解码（`ParseJSONStrict`、`decodeAuthBody`），未知字段直接
  `400`；Agent 版本化协议与公共交互端点继续使用允许前向字段的宽松解码；
- `internal/api/middleware.JSONBodyLimit` 在最外层把声明为 JSON 的请求体装进
  `http.MaxBytesReader`，位置在认证之前；上传、归档与主题包等非 JSON 载荷继续使用各自路由
  声明的预算，不受影响；
- 全部 38 处旧调用点已迁移：错误分支改为 `WriteBodyError`，复合条件（排序数组为空、
  受管节点字段非法、Agent 结果条数越界）拆成“先判解码、再判语义”。

证据：`internal/api/handler/body_test.go` 覆盖六类失败原因与状态码、超限、空体、取消与
未知字段严格/宽松两种模式；`internal/api/middleware/json_body_test.go` 证明只有 JSON 请求被
限流包装；`internal/api/handler` 与 `internal/api` 用例在跳过本沙箱无法创建 socket 的用例后
通过；`go build ./...`、`go vet`、`gofmt`、`goimports-reviser`、`golangci-lint`（0 issues）干净。

同批收紧公共节点历史的请求预算：显式 `metric` 选择在触碰 MTS 之前即要求 1 到 8 个不重复
指标，区间必须是正区间、不超过 31 天且不是纯未来区间，否则返回 `400`；默认（未显式选择）
路径仍返回目录中的公开指标集，并受同一区间约束。

第四批落地公共节点历史的 MTS 侧分桶聚合（仍为 schema-free）：

- `metricquery.PlanBuckets` 从请求区间推导确定性桶宽：按秒向上取整，最小 1 秒，桶数不超过
  600；非法区间（非正、超过 31 天、点预算越界）直接失败；
- `MTSStore.QueryBucketedMetrics` 在读取迭代过程中按桶聚合（`avg`/`min`/`max`/`sum`/`last`/
  `rate`），只返回有值的桶，因此响应点数由预算而不是原始样本数决定，不再把全部原始行取回
  再在 Go 里降采样；
- 每个指标的聚合函数来自目录定义的默认聚合（尊重运营方通过 `PUT /api/v1/admin/metrics/catalog/{key}`
  的更新），没有目录时退回 `metriccatalog.DefaultAggregationForKey` 的内置默认值（gauge →
  `avg`，计数器 → `last`，流量派生 → `sum`）；
- 显式选择仍是 1–8 个不重复指标；默认（未显式选择）路径返回全部公开指标，按每次最多 8 个
  指标分块读取并合并，保持单次查询的指标预算不变；
- `available_from` 语义、`304` 与准入类别不变：分桶查询仍走全局 MTS 查询信号量、每客户端租约
  与查询超时。

证据：`internal/metricquery/plan_test.go` 覆盖 1 分钟/1 小时/30 天/31 天/亚秒与非法区间；
`internal/store/mts_public_history_test.go` 覆盖桶内聚合、空桶跳过、超预算拒绝与关闭后失败；
`internal/api/handler` 用例证明公共历史响应落在 600 点预算内且桶值正确。

第五批落地前端对拒绝语义的配合（无后端变更）：

- `webui/src/lib/retry-after.ts` 是唯一的退避策略入口：`retryAfterDelay` 从 `APIError` 取
  `Retry-After` 秒数并封顶 300 秒，`pollDelay` 取「服务端要求」与「本地间隔」的较大值；
- `useApiList.refresh` 与 `useApiPage.refresh` 现在返回服务端要求的退避毫秒数（成功为 `0`），
  轮询方据此调整下一次请求，而不是继续按固定间隔重试；
- 由固定间隔改为超时链并遵守退避的轮询：节点指标曲线（10 秒）、网络任务视图与网络历史
  （15 秒）、通知投递（5 秒）、安全事件（15 秒）、诊断资源、操作资源、Agent 发布数据、
  应用密钥轮转状态、维护与保留任务面板（2 秒）、指标擦除任务面板（5 秒）、公共节点摘要与
  公共舰队实时快照、以及指标工作区轮询器（改用同一策略）；
- 前端只做退避，不放宽服务端的准入判断：`429`/`503` 仍然由 `Retry-After` 或常规间隔驱动重试，
  暂停与页面隐藏时依旧不发请求。

证据：`webui/src/lib/retry-after.test.ts` 覆盖无退避、服务端退避、封顶与非法值；
`use-api-list-reliability.test.tsx` 证明 `refresh` 回传退避毫秒数；前端 `tsc -b`、`oxlint`、
`vitest`（175 文件 / 912 用例）与 `npm run build` 全部通过。

第六批落地公共历史的在途读取合并（仍为 schema-free）：

- `internal/api/handler/history_share.go` 是一个按读取输入取键的 `singleflight`：第一个到达的
  请求执行存储读取，同键的其他请求等待同一结果，而不是每个观看者各打一次 MTS；节点指标历史
  与公共/管理网络历史各持一个实例，键互不串用；
- 合并键包含节点（或任务与节点）、区间、指标集合、桶宽与桶数，只有输入完全相同的读取才会
  命中同一条记录；指标顺序在键中保持调用方给出的稳定顺序；
- 合并表上限 `historyShareCapacity = 128`，表满时新读取直接由调用方执行，只降低合并收益，
  不改变正确性；等待方始终尊重自己的请求上下文，取消时返回 `context.Canceled`，
  leader 结束后记录立即释放，失败结果不会驻留在表中；
- 准入与预算没有放宽：每个请求仍各自通过 `metricquery.ReadWithBudget` 取号，合并只发生在
  取号之后的存储读取层，因此单客户端配额与全局 MTS 并发上限语义不变；
- `NodeHandler`/`NetworkHandler` 构造时初始化合并表；以结构体字面量构造（测试或旧调用点）
  时为 nil，nil 接收者退化为直读。

证据：`internal/api/handler/history_share_test.go` 覆盖同键并发只读一次、不同键互不影响、
表满退化、等待方取消返回 `context.Canceled`、读取错误传播与键释放、nil 接收者直读与键的
字段边界；`go build ./...`、`go vet`、`gofmt`、`goimports-reviser`、`golangci-lint`（0 issues）
干净，`internal/api/handler`、`internal/api`、`cmd/server`、`internal/store`、
`internal/metricquery`、`internal/metriccatalog` 定向用例（跳过本沙箱无法创建 socket 的用例）
通过。

第七批落地公共网络质量视图的共享快照（仍为 schema-free）：

- `HandlePublicNetworkQuality` 不再每个观看者各建一份视图：最近一次成功构建的视图放进
  `atomic.Pointer`，在 5 秒复用窗口内直接复用，同时到达的观看者再由
  `networkQualityShare` 合并成一次构建（`internal/api/handler/network_quality_snapshot.go`）；
- 视图代 `qualityGeneration` 在任务创建/更新/排序/删除与探测结果写入后自增，因此任何写入
  都会立即失效上一代视图，而不是等窗口过期；窗口只吸收并发观看者，不推迟自己的写入；
- 视图中的节点标识与探测结果都是绝对时间戳（由客户端渲染相对时间），所以复用不会把上一代
  的相对数值当作当前值；节点可见性变更不经由本处理器，最多在复用窗口内保持上一代结果，与
  第二批公共节点快照的窗口语义一致；
- 失败构建不会覆盖上一份可用视图：窗口内继续复用，窗口过后才回落到既有的
  `failed to list network quality` / `failed to load network quality` 500 文案；
- 响应带上由视图代生成的 `ETag` 与 `Cache-Control: no-cache`，`If-None-Match` 命中返回 304。

Fleet 状态摘要不需要新的服务端缓存：公共舰队页的在线/离线、分组、国家与速率汇总由前端在
共享的公共节点快照（第二批 `GET /api/v1/nodes`）上计算，本批因此不再为它引入第二份快照。

证据：`internal/api/handler/network_quality_snapshot_test.go` 覆盖窗口内复用与过期重建、写入
失效（任务创建/更新/排序/删除与探测结果）、`If-None-Match` 304 与陈旧校验器、并发观看者
只构建一次、存储失败后保留上一份视图并在窗口过后返回有界文案；`-race -count=5` 通过。

未完成（仍属本批）：指标告警与容量演练，以及宿主机上的负载/浸泡与浏览器可访问性证据。

同批还修正了三处历史遗留的删除用例：节点与网络任务删除自 `v22` 起是异步擦除任务
（`202` + 耐久任务视图、`request_id` 必须是 UUID、`confirmation` 必须等于服务端解析出的
目标显示名），`internal/api/handler` 的 `TestHandleDeleteNode`、`TestNodeHandlerDatabaseErrors`、
`TestNetworkHandlerWorkflow`、`TestNetworkHandlerRejectsInvalidRequests`、
`TestNetworkHandlerStoreFailures` 现在按该契约断言，服务端校验未被放宽。
