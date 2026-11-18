# Beat 网络节点指标采集平台设计方案

## 1. 背景与目标

Beat 用于监控物理机、边缘节点、网关和服务节点的运行状态。项目选择自研而非直接使用 Prometheus + Grafana，原因如下：

1. **Prometheus 是单值模型**：一个样本只描述一个数值。节点同一时刻的 CPU、内存、磁盘、网络等指标通常会被拆成多个 series，采集与查询链路需要重复拼接。
2. **Grafana 组件较重**：完整监控栈需要 Prometheus、Grafana、Alertmanager 等多个组件，部署与维护成本超出轻量节点监控场景。
3. **业务需要嵌入式交付**：Beat 希望以 Go 单二进制交付，控制台、存储、采集协议与服务端逻辑形成闭环。

MTS 是 Go 实现的单机嵌入式多值模型时序数据库。一个 `Point` 可以携带多个字段，非常契合“节点 + 时间点 + 多维指标”的模型。

### 1.1 一期目标

- 实现 `beat-server` 与 `beat-agent`。
- Agent 采集节点指标，并通过 gRPC 双向流上报。
- Server 将指标写入 MTS，将关系型数据写入 SQLite。
- 提供前后台分离的 Web 控制台：前台按分组展示节点广场与指标详情，后台运行总览、节点管理、分组管理、节点接入、任务中心和系统设置。
- 完成节点注册、状态判定、代理接入与指标查询闭环。
- 通过任务中心向 Agent 下发管理员定义的三网探测或 Shell 周期任务，并回传执行结果。

### 1.2 一期不做

- 分布式存储和 Server 集群。
- PromQL / SQL 查询语言。
- 完整 Grafana 式仪表盘编辑器。
- 告警规则、通知渠道和告警事件流。
- 多租户、复杂 RBAC、SSO。
- Agent 后台定时升级、SSH 远程管理与交互式远程 Shell；升级只由 Server 主动下发，任务中心只执行管理员配置的模板任务或周期脚本。

## 2. 总体架构

```text
┌──────────────────────────── 被监控节点 ────────────────────────────┐
│                                                                  │
│  beat-agent                                                       │
│  ├── /proc 指标采集器（CPU / MEM / DISK / NET / LOAD / PROC）      │
│  ├── 本地 Agent ID 持久化                                          │
│  ├── 多值指标快照组装                                              │
│  ├── 双并发槽周期任务执行器与结果回传                                │
│  └── gRPC 双向流客户端（Bearer Token + 自动重连）                    │
│                                                                  │
└──────────────────────────────┬───────────────────────────────────┘
                               │ gRPC / HTTP2 bidirectional stream
                               │ beat.agent.v1.AgentControl/Connect
                               ▼
┌──────────────────────────── beat-server ───────────────────────────┐
│                                                                  │
│  HTTP API (chi)                                                   │
│  ├── 总览 / 节点 / 分组 / 指标查询                                   │
│  ├── 任务中心 / 代理 / 设置                                         │
│  └── 内嵌 React SPA                                                │
│                                                                  │
│  Agent gRPC / h2c                                                 │
│  ├── 双向流认证与连接注册                                          │
│  ├── 指标消息接收 / 任务即时推送                                    │
│  └── 执行结果与卸载回执                                             │
│                                                                  │
│  应用层                                                            │
│  ├── 节点注册与状态判定                                             │
│  ├── 指标写入编排                                                   │
│  ├── 查询范围解析与降采样                                           │
│  ├── 任务定义与周期窗口调度                                         │
│                                                                  │
│  存储层                                                            │
│  ├── SQLite：节点 / Agent / 任务 / 设置                            │
│  └── MTS：多值节点指标时序数据                                      │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
                               ▲
                               │ JSON API
                               │
┌──────────────────────────────┴───────────────────────────────────┐
│  React + TypeScript + shadcn 风格组件                              │
│  前台：节点广场 / 指标详情                                          │
│  后台：运行总览 / 节点管理 / 分组管理 / 节点接入 / 系统设置            │
└──────────────────────────────────────────────────────────────────┘
```

### 2.1 数据流

1. Agent 从 `/proc` 和 `statfs` 采集原始计数器。
2. Agent 计算本周期 CPU 使用率、网络速率等速率型指标。
3. Agent 建立一条 gRPC 双向流，并在每次采集周期发送 `Report` 消息。
4. 管理员先按目标节点架构通过 Agent 安装包完成下载安装，再生成短期注册令牌与对应的启动接入命令。
5. Agent 使用注册令牌上报；Server 校验令牌有效期并绑定首个使用的 `agent_id`。
6. Server 更新 SQLite 中节点的最新快照、元数据和心跳。
7. Server 将该快照作为一条 MTS `Point` 写入 `node_metrics` measurement。
8. 前台与后台按范围查询 MTS，并在 Server 侧进行展示用降采样。
9. Server 在同一双向流上推送 `TaskDispatch`；Agent 在独立执行槽中运行探测或脚本，并通过 `TaskResult` 回传。
10. 管理员卸载 Agent 时，Server 删除 SQLite 节点、MTS 时序数据和注册凭据，写入 `uninstall_agent` 任务与 Agent 墓碑，并立即向在线 Agent 推送任务；Agent 在本地删除命令、服务配置与数据后回执。

## 3. 指标模型

### 3.1 MTS Measurement

```text
measurement: node_metrics
tags:        node_id = <server-side-node-id>
timestamp:   nanoseconds
fields:
  cpu_usage           float64   %
  cpu_user            float64   %
  cpu_system          float64   %
  cpu_iowait          float64   %
  memory_used_bytes   float64
  memory_total_bytes  float64
  memory_usage        float64   %
  swap_used_bytes     float64
  disk_used_bytes     float64
  disk_total_bytes    float64
  disk_usage          float64   %
  network_rx_bps      float64   bytes/s
  network_tx_bps      float64   bytes/s
  load1               float64
  load5               float64
  load15              float64
  processes           float64
  tcp_connections     float64
  uptime_seconds      float64
```

一个时间点即一条完整节点快照。相比单值模型，读取一个节点某一时刻的资源状态只需一条记录，查询与列式存储更自然。

### 3.2 SQLite 关系模型

| 表 | 作用 | 关键字段 |
|---|---|---|
| `nodes` | 节点当前状态与元数据 | `id`, `agent_id`, `hostname`, `status`, `latest_metrics_json`, `last_seen` |
| `node_groups` | 节点业务/地域分组 | `id`, `name`, `description`, `color`, `updated_at` |
| `node_group_members` | 节点与分组的单值归属关系 | `node_id`, `group_id`, `assigned_at` |
| `agents` | Agent 生命周期信息 | `id`, `node_id`, `version`, `interval_seconds`, `last_seen` |
| `agent_enrollment_tokens` | 短期注册令牌 | `token_hash`, `expires_at`, `used_at`, `bound_agent_id` |
| `task_definitions` | 管理员任务定义 | `id`, `source`, `script`, `interval_seconds`, `timeout_seconds`, `enabled` |
| `task_template_configs` | 模板任务类型与结构化配置 | `task_id`, `kind`, `config_json`, `updated_at` |
| `task_runs` | 每个 Agent、每个周期窗口的下发与执行记录 | `task_id`, `agent_id`, `scheduled_for`, `status`, `output`, `exit_code` |
| `public_theme_revisions` | 前台主题的不可变草稿、发布与归档版本 | `revision`, `status`, `document_json`, `created_at`, `published_at` |
| `settings` | 系统设置键值表 | `key`, `value` |

SQLite 使用 WAL、`busy_timeout` 和单连接写入，适合一期单机部署。后续可在读多写多场景中引入读连接池或 PostgreSQL。

## 4. Agent 设计

### 4.1 采集来源

| 指标 | 数据源 |
|---|---|
| CPU 使用率、用户态、系统态、iowait | `/proc/stat` 差值 |
| 内存、Swap | `/proc/meminfo` |
| 磁盘容量与使用率 | `statfs(BEAT_DISK_PATH)` |
| 网络累计流量与速率 | `/proc/net/dev` 差值 |
| Load Average | `/proc/loadavg` |
| 进程数 | `/proc` 数字目录计数 |
| TCP 连接数 | `/proc/net/tcp`、`/proc/net/tcp6` |
| 运行时长 | `/proc/uptime` |
| 操作系统 | `/etc/os-release` |
| 内核版本 | `/proc/sys/kernel/osrelease` |
| CPU 型号 | `/proc/cpuinfo` |
| 网卡地址与 MAC | `net.Interfaces()`（仅内部存储，不通过节点查询 API 返回） |

### 4.2 Agent 生命周期

1. 读取 `--state-file` 中的 Agent ID；不存在时生成随机 ID 并持久化。
2. 建立 gRPC 双向流，发送 `Hello` 并等待 `Welcome`；认证令牌通过 gRPC metadata 传递。
3. 连接成功后立即采集并上报一次，之后按 `--interval` 周期发送 `Report`，保持上一周期计数器以计算速率。
4. 接收循环在独立 goroutine 中持续读取 Server 消息；任务进入容量为 128 的本地队列，由两个 worker 并发执行，不阻塞指标采集。
5. 周期脚本通过 `/bin/sh -c` 执行，结果通过同一流发送 `TaskResult`；Server 返回 `TaskResultAck`。
6. 连接异常时采用 1–30 秒指数退避重连；已完成结果在内存中去重并可在同一进程重连后重发。
7. 收到 `uninstall_agent` 后执行本地清理，回传结果并退出进程；任务队列满时拒绝新任务并回传失败结果。
8. 收到 `AgentUpgrade` 控制消息后下载目标架构二进制、校验 SHA-256、自检并原子替换自身，使用专用退出码交由 systemd 重启；Agent 不进行定时版本检查。
9. 由 systemd 等进程管理器负责异常退出后的重启。

### 4.3 gRPC 双向协议

Agent 数据面不再提供 HTTP 上报与回执接口。Proto 定义位于 `proto/beat/v1/agent.proto`，服务为：

```proto
service AgentControl {
  rpc Connect(stream AgentMessage) returns (stream ServerMessage);
}
```

连接建立时通过 metadata 传递 `authorization: Bearer <token>`。首条消息必须是 `Hello`，Server 校验令牌、Agent 身份和卸载墓碑后返回 `Welcome`，并将连接注册到在线 Agent 会话表。

| 方向 | 消息 | 用途 |
|---|---|---|
| Agent → Server | `Hello` | Agent ID、主机名、版本握手 |
| Server → Agent | `Welcome` | 服务端版本、并发任务上限、心跳周期 |
| Agent → Server | `Report` | 多值指标、标签、网卡明细与节点元数据 |
| Server → Agent | `TaskDispatch` | 三网探测配置、周期脚本或即时卸载任务 |
| Agent → Server | `TaskResult` | 状态、退出码、耗时和输出 |
| Server → Agent | `TaskResultAck` | 结果持久化确认 |
| 双向 | `Heartbeat` | 长连接存活探测 |

典型消息序列：

```text
Agent                         Server
  | ---- Hello --------------> |
  | <--- Welcome ------------- |
  | ---- Report -------------> |  写入 SQLite + MTS，并调度任务
  | <--- TaskDispatch -------- |
  | ---- TaskResult ---------> |  更新 task_runs / agent_tasks
  | <--- TaskResultAck ------- |
  | ---- Report -------------> |
  | <--- TaskDispatch -------- |  管理员卸载节点时即可立即推送
```

Web 控制台仍使用 `9099` 上的 HTTP/1.1；Agent 使用同一端口上的 gRPC/h2c。HTTP Handler 根据 HTTP/2 与 `application/grpc` Content-Type 将请求分流到 gRPC Server，因此单个端口即可同时提供 SPA、管理 API 和 Agent 长连接。

`interfaces` 属于敏感网络拓扑信息，仅作为 Agent 上报字段写入服务端内部存储；`/api/v1/nodes`、`/api/v1/nodes/{id}` 和总览接口的节点响应不会包含网卡名称、地址或 MAC。

## 5. Server 设计

### 5.1 模块划分

```text
cmd/beat-server
internal/server          HTTP API、gRPC Agent 服务、查询、降采样、Demo
internal/protocol/beatv1 gRPC Proto 生成代码与领域模型转换
internal/domain          节点、指标与代理领域模型
internal/platform/sqlite SQLite 存储与迁移
internal/platform/tsdb   TSDB 接口与 MTS 实现
web                      React SPA 与 embed
```

### 5.2 节点状态判定

```text
offline  : now - last_seen > max(3 × interval, 45s)
warning  : CPU ≥ 90% 或 内存 ≥ 90% 或 磁盘 ≥ 90%
online   : 其他仍在上报的节点
```

状态为展示层派生值，节点最新原始快照始终保存在数据库中。

### 5.3 查询与降采样

| 范围 | 展示步长 |
|---|---|
| 1h | 1m |
| 6h | 5m |
| 24h | 15m |
| 7d | 1h |

一期由 Server 查询 MTS 原始行后做均值/求和降采样。后续查询量上升后应切换为：

- MTS `Aggregate + GroupByTime` 聚合查询；
- MTS downsample policy 预聚合；
- 按节点和时间范围限制最大样本数。

### 5.4 接入令牌与二进制分发

- 每次点击“生成接入命令”都会产生新的 256-bit 随机令牌，默认 10 分钟有效；接入命令将 Server、令牌、节点名称、标签、状态文件和采集周期写入权限为 `0600` 的 `/etc/beat/agent.yaml`，由 systemd 使用 `--config /etc/beat/agent.yaml` 加载。旧 `/etc/beat/beat-agent.env` 不再被服务读取，接入命令会主动清理该文件。
- SQLite 只保存令牌的 SHA-256 摘要，明文令牌仅在生成响应中出现一次。
- 未绑定令牌超过有效期后拒绝接入；首次成功上报后令牌绑定到对应 `agent_id`，该 Agent 的持续心跳不受注册窗口关闭影响。
- Agent 二进制不再经由 Server 下载接口分发。GitHub Release 提供 `beat-agent-linux-amd64`、`beat-agent-linux-arm64` 与 `beat-manifest.json`；Server 读取清单中的版本、文件大小、SHA-256 与 GitHub 地址，生成一键安装和批量升级指令。安装命令会校验主机架构与摘要，创建 `/etc/beat`，将二进制直接安装为 `/usr/local/bin/beat-agent`，并生成 root 运行的 `/etc/systemd/system/beat-agent.service`。Agent 身份文件 `agent-id` 与 YAML 配置一起保存在 `/etc/beat`。
- Server 二进制内置 `beat-server install [--no-start]` 管理命令，仅允许 root 执行并使用固定路径：安装 `/usr/local/bin/beat-server`、生成 `/etc/beat/server.yaml` 和 `/etc/systemd/system/beat-server.service`，随后执行 `systemctl enable`；默认同时启动服务。安装器通过文件身份判断二进制是否已经位于目标路径，已安装时只校正权限，不重复复制或替换 inode。已有配置不会被覆盖，配置权限会收紧为 `0600`。`deploy/install-server.sh` 是引导该命令的薄封装，并通过 `BEAT_START_NOW=false` 支持只安装不启动。
- Agent 二进制内置 `beat-agent install` 管理命令，仅允许 root 执行，并且目标路径固定；它负责创建目录、复制自身、创建 `/usr/local/bin/beat-agent`、初始化 `/etc/beat/agent.yaml` 和 systemd 单元，然后执行 `daemon-reload`。已有 YAML 配置不会被安装命令覆盖，配置权限会被收紧为 `0600`。
- Agent 启动配置优先级为命令行参数 > YAML > 兼容的 `BEAT_*` 环境变量 > 默认值；`--config` 和 `BEAT_AGENT_CONFIG` 均可指定 YAML 路径。
- 注册窗口默认 10 分钟，可通过 `--enrollment-token-ttl` 在 1 分钟至 24 小时之间调整；Agent 发布源通过 `agent_release_repository` 与 `agent_release_tag` 配置，正式部署默认读取 `openmts/beat` 的 `latest` Release，nightly 部署读取 `nightly`。`--agent-binary-dir` 仅作为本地开发环境的元数据回退。

### 5.5 系统设置

- 站点名称、副标题、默认时区与对外访问地址保存到 SQLite `settings` 表，保存后立即用于前后台导航、浏览器标题和 Agent 接入命令。
- MTS 数据目录、保留周期和 WAL 同步落盘策略同样持久化到 SQLite。由于嵌入式 MTS 引擎在 Server 启动时打开，这些参数保存后需要重启生效。
- 启动参数以 `--listen`、`--data-dir`、`--demo`、`--agent-release-repository`、`--agent-release-tag`、`--tls-cert` 与 `--tls-key` 为主，也可通过 `--config` 加载 `/etc/beat/server.yaml`；优先级为命令行参数 > YAML > 兼容的 `BEAT_*` 环境变量 > 默认值。SQLite 路径从数据根目录推导，MTS 默认目录和采集策略使用默认值或系统设置。

### 5.6 控制台认证

- 控制台采用单管理员账号模型，首次启动自动写入 `admin/admin`；账号和密码均可通过系统设置修改。
- 密码使用 PBKDF2-HMAC-SHA256、随机盐和 120,000 次迭代哈希后保存到 SQLite `settings` 表，不保存明文。
- 登录成功后签发 256-bit 随机会话令牌，通过 `HttpOnly`、`SameSite=Lax` Cookie 传递，默认 12 小时有效；SQLite `admin_sessions` 仅保存令牌摘要、CSRF Token 与凭据版本，Server 重启后会话仍然有效。
- 凭据包含递增版本号。修改账号/密码或执行 `beat-server reset-admin` 后会删除持久会话并递增版本，已有会话立即失效；设置页更新凭据后会为当前浏览器轮换新会话。
- 所有管理员写请求必须携带会话对应的 `X-Beat-CSRF`；服务端同时校验 `Origin` 与 `Sec-Fetch-Site`。登录失败按来源地址限流，管理写操作写入 90 天审计日志。
- 安全响应头包含 CSP、frame denial、nosniff、Referrer-Policy、Permissions-Policy；启用原生 TLS 时附加 HSTS。
- 前台只通过 `/api/v1/public/*` 读取公开投影；`/api/v1/nodes`、`/api/v1/groups` 等内部接口包含 Agent 标识或管理字段，统一要求管理员会话。公开投影不返回 `agentId`、Agent 版本和网络接口信息。
- `beat-server reset-admin` 在 Server 停止或运行状态下都可执行，将账号密码恢复为 `admin/admin`。

### 5.7 节点配置与卸载任务

- 节点编辑写入独立的 `node_configs` 表，管理侧名称和标签覆盖 Agent 上报值，但不会改写 Agent 启动参数或采集周期。
- 卸载采用异步任务模型：SQLite `agent_tasks` 保存 `uninstall_agent` 任务，`agent_tombstones` 阻止已删除 Agent 通过旧身份重新创建节点。
- Agent 在线时，Server 通过已建立的 gRPC 双向流立即推送卸载任务；离线时任务保持 `pending`，重新连接后执行。任务完成后记录 `success`/`failed`，成功时同时吊销该 Agent 的注册令牌。
- 本地卸载器只处理标准路径：systemd unit、`/etc/beat/agent.yaml`、`/etc/beat/agent-id`、旧 `/etc/beat/beat-agent.env`、`/usr/local/bin/beat-agent` 与旧 `/var/lib/beat-agent` 目录。非标准安装路径会被拒绝删除。

### 5.8 任务中心

- 任务由管理员在控制台选择模板后创建。当前提供“三网时延与丢包检测”和“自定义 Shell 脚本”两类；支持编辑和删除，删除定义后保留历史执行记录。
- 三网探测配置保存在 `task_template_configs.config_json`，包括电信、联通、移动三个目标的主机/IP、端口、地址族，以及探测次数、间隔和超时。`TaskDispatch.config_json` 将该配置原样下发给 Agent。
- 三网任务默认使用 TCP Connect：以三次握手完成时间作为 RTT。Agent 原生执行探测，不依赖系统 `ping`/`mtr` 的文本输出；每个目标返回两个独立维度：`loss`（发送、接收、丢失、失败、丢包率）与 `latency`（可用性、成功样本数、最小/平均/最大/P50/P95、抖动）。
- 单次握手超时/无响应记为 `lost`，连接拒绝/重置等记为 `failed`，两者分开统计；DNS 解析失败时两个维度均不可用。时延只基于成功样本计算；全部丢包时 `latency.available=false` 且时延字段全部为 `null`，避免用 `0 ms` 误表示“无法测量”。逐次 `samples` 同样使用 `rttMs: null` 表示丢失或失败。
- 任务结果进入 Server 后，完整 JSON 保存在 SQLite `task_runs.output`，同时将每个运营商目标展开写入 MTS `network_probe` 测量。标签为 `node_id/task_id/task_name/carrier/label`，字段包括丢包可用性、丢包率、发送/接收/丢失/失败计数，以及时延可用性、Min/Avg/Max/P50/P95/Jitter；时延不可用通过 `latency_available=0` 保留语义。
- 节点详情通过公开或后台对应的 `network-probes` 接口查询并聚合 MTS 时序，按任务和运营商返回对齐的 `timestamps` 与可空指标数组。前端使用三网时延、三网丢包率两张趋势图及当前快照展示，空值时断线并显示“不可测”。
- 任务启用后对所有已纳管 Agent 生效。每次 Agent 指标上报成功后，Server 以 `floor(now / interval)` 生成周期窗口，并为每个启用任务写入一条 `task_runs` 记录；`(task_id, agent_id, scheduled_for)` 唯一约束保证同一 Agent 在同一窗口最多接收一次。
- Server 通过同一条 gRPC 双向流即时发送 `TaskDispatch`，Agent 无需额外轮询；卸载等控制任务不依赖指标上报周期，可立即推送到在线 Agent。任务执行与指标上报相互独立，脚本失败不会阻止下一次采集或后续任务。
- Agent 使用 `/bin/sh -c` 执行脚本，通过独立进程组和超时控制强制终止残留子进程；标准输出与标准错误合并，单次最多保留 64 KiB。
- Agent 使用两个并发执行槽，任务队列容量为 128；队列满时立即回传失败结果。任务回执包含 `success`、`failed` 或 `timeout`、退出码、耗时和输出。Server 按连接身份校验后更新执行记录，控制台可查看完整输出。
- 一期任务不做失败重试、指定节点/分组下发和脚本签名。脚本以 Agent 进程用户权限执行；若 Agent 以 root 运行，脚本同样拥有 root 权限，因此任务只应开放给可信管理员。

### 5.9 Agent 批量升级

- 管理页面创建升级批次后，Server 从 SQLite 读取节点版本、状态和架构，计算旧版本节点；目标版本固定为 Server 当前发布的 Agent 版本，浏览器不能提交下载地址或校验值。
- 新增 `AgentUpgrade` 与 `UpgradeEvent` gRPC 消息。Agent 仅在收到已认证 Server 消息后执行升级，不提供后台检查、版本轮询或 update daemon。
- Agent 校验指令中的 GitHub Release 下载地址、仓库、架构、版本、大小与 SHA-256；下载重定向只允许 `github.com` 与 `*.githubusercontent.com`，并且只允许更新标准路径 `/usr/local/bin/beat-agent`，开发目录直接拒绝。
- 新二进制先完成 `--version` 自检，再同目录原子替换并保留 `.previous`；Agent 以退出码 75 请求 systemd 重启。Server 通过重新连接时 `Hello.version` 等于目标版本确认成功，不把升级过程中的短暂断连视为失败。
- `agent_upgrade_batches` 和 `agent_upgrade_items` 保存批次、节点、原版本、目标版本、校验值、尝试次数和进度。最多 10 个节点同时处于已下发状态，离线节点保持排队，失败节点可人工重试。
- 所有升级写操作沿用管理员会话、CSRF、跨域拦截和审计日志。Agent 以 root 运行，以便原子替换 `/usr/local/bin/beat-agent`、安装 systemd 单元并执行本地卸载；不存在 SSH 凭据或远程 Shell 通道，升级下载地址限定为配置仓库的 GitHub Release 固定资产。

### 5.10 前台主题与展示解耦

- 前台主题独立于系统设置，使用 `public_theme_revisions` 保存不可变 revision。`draft`、`published` 各有唯一记录，发布时先将旧线上版本归档，再切换新版本；前台只读取 `published`，没有记录时回退到内置默认主题。
- 主题文档使用 `internal/publictheme` 的受控 Schema。允许的模块类型、ID 唯一性、颜色格式、布局枚举、模块数量与 JSON 体积均在服务端校验，不接受任意 HTML、CSS 或 JavaScript。
- 主题分为 `appearance` 与 `home`、`nodeDetail` 两个页面。每个模块包含 `id`、`type`、`enabled`、`layout` 和受控 `props`。完整配置由 Beat 主题包统一维护，后台只提供主题选择、首页/详情预览与一键应用，不向用户暴露逐个模块的调参界面。
- 前端 `features/public` 分为 theme、data、renderer、blocks、pages：主题 Provider 只在公开根节点写入 CSS 变量；主题文档驱动 Block 注册表渲染，每个模块有独立错误边界；页面层只负责查询公开数据与筛选状态。
- 前后台数据权限解耦先于主题渲染：`PublicNode`、`PublicGroup` 等 DTO 在服务端显式投影，主题无法通过模块配置扩大数据可见范围。内部节点与分组读取接口要求管理员会话。
- 预览直接读取 Public API 的真实数据，并使用选中主题包的布局与变量；应用只切换线上 revision。当前内置 `minimal`（极简）和 `beat-default`（Beat 默认主题）两套完整主题，后续主题作为完整主题包加入目录。极简主题通过模块 `variant` 提供紧凑四列卡片、黑灰配色与无阴影容器，原 Beat 主题继续使用渐变背景和标准卡片，后续可以继续扩展模块类型、页面级排版和社区展示，而无需改动内部节点 API。

## 6. 控制台设计

### 6.1 页面信息架构

```text
前台（无侧栏，面向日常巡检）
├── 节点卡片广场
│   ├── 节点总数 / 在线 / 高负载 / 离线摘要
│   ├── 搜索与状态筛选
│   └── 节点卡片（CPU / 内存 / 磁盘 / 网络 / 标签 / 最后上报）
└── 节点指标详情
    ├── CPU / 内存 / 磁盘 / 网络
    ├── 时序趋势（1h / 6h / 24h / 7d）
    ├── 系统信息
    ├── 负载与进程
    └── 采集与标签

后台（侧栏管理台）
├── 运行总览
├── 节点状态摘要
├── 集群资源趋势（1h / 6h / 24h / 7d）
├── 节点状态环与 Top CPU
├── 节点概览表
└── 采集与代理概况

节点管理
├── 状态筛选与搜索
├── 节点资源表
├── 节点所属分组标识
├── 三点操作菜单
├── 编辑节点名称、标签与分组
├── 卸载 Agent 二次确认与任务下发
└── 节点详情
    ├── CPU / 内存 / 磁盘 / 网络
    ├── 时序趋势
    ├── 系统信息
    ├── 负载与进程
    └── 采集与标签

分组管理
├── 分组数量、已分组与未分组节点概览
├── 分组卡片与在线/高负载/离线统计
├── 创建、编辑识别色与描述
├── 批量选择分组成员
├── 删除分组并保留节点
└── 未分组节点入口

节点接入
├── x86_64 / ARM64 架构切换
├── Agent 一键下载与安装命令（统一安装为 beat-agent）
├── 短期注册令牌生成与倒计时
└── 一键接入命令

任务中心
├── 三网时延与丢包检测 / 自定义 Shell 任务模板
├── 三网目标的运营商、主机/IP、端口、地址族与探测参数
├── 任务周期、超时、最近执行和成功率
├── 新建、编辑、删除任务
└── 最近执行、三网结构化结果与原始输出

系统设置
├── Server 信息
├── 采集策略
├── 存储拓扑
└── 一期能力边界
```

### 6.2 视觉语言

参考 `virtmgr` 控制台：

- 浅蓝灰页面底色 `hsl(216 33% 97%)`；
- 深蓝主色 `hsl(214 90% 43%)`；
- 白色卡片、细边框、轻阴影；
- 248px 紧凑侧栏与 64px 顶栏；
- 绿色表示在线，琥珀色表示高负载，红色表示离线；
- 支持明暗主题切换；
- 图表使用自定义 SVG，避免为简单图表引入重型可视化依赖。

## 7. 部署与容量

### 7.1 一期部署

```text
beat-server（单机）
├── HTTP/1.1 + gRPC h2c [::]:9099
├── SQLite ./data/beat.db
└── MTS ./data/mts

每台被监控节点
└── beat-agent（systemd 托管）
```

### 7.2 容量估算

假设 100 个节点、15 秒采集、单个快照约 20 个字段：

- 每秒写入点：`100 / 15 ≈ 6.7 points/s`
- 每天写入点：约 `576,000 points/day`
- 每个点包含多值字段，MTS 的列式编码和压缩可显著降低存储体积

一期使用 MTS 的 WAL、SSTable compaction 和 retention。生产部署需通过实际字段数量和压缩配置压测后确定磁盘预算。

### 7.3 后续扩展

- SQLite → PostgreSQL：多 Server / 高并发写。
- MTS 本地目录 → 独立存储节点：一期不改协议，仅替换 TSDB 适配层。
- SSE / WebSocket：替代控制台轮询，实时推送节点状态与指标变化。

## 8. 安全设计

一期已实现：

- Agent gRPC 双向流在握手阶段校验 metadata 中的 Bearer Token（常量时间比较），并校验消息中的 Agent 身份与连接身份一致；
- 控制台单管理员登录、PBKDF2 密码哈希、持久 HttpOnly 会话、CSRF/同源校验、登录限流与凭据版本失效机制；
- CLI 管理员凭据重置命令；
- 任务脚本仅允许管理员创建，Agent 回执需匹配已下发记录和 Agent 身份；
- 任务脚本 16 KiB、输出 64 KiB、执行超时 1–300 秒的边界限制；
- 请求体 2 MiB 限制与未知字段拒绝；
- SQLite 参数化查询；
- 管理员写操作审计、90 天审计保留、30 天任务记录保留、过期会话/令牌每小时清理；
- 可选原生 TLS，控制台与 gRPC 复用安全端口；Agent 支持系统 CA 校验和显式 `--insecure-skip-verify`；
- Agent 安装包 SHA-256 校验、状态文件 0600 权限与 `doctor` 自检；
- 服务端与 MTS 仅监听本机或受控地址（由部署配置决定）。

一期缺口：

- 控制台暂无多账号与 RBAC；
- 已绑定 Agent 的令牌尚不支持后台吊销和主动轮换；
- Shell 任务尚无脚本签名、审批流与沙箱；
- 审计日志暂无管理查询界面。

生产加固建议：

1. 控制台接入多账号与 RBAC，并提供审计日志查询界面。
2. 增加 Agent 独立凭据、吊销和主动轮换能力。
3. 在多节点部署中由受控负载均衡器统一终止 TLS，并配置可信代理来源。
4. 增加任务脚本签名、审批流与失败重试。

## 9. 路线图

### Phase 1：生产采集闭环（当前版本）

- Agent gRPC 双向流上报与即时任务下发；
- SQLite + MTS 双存储；
- 前台按分组筛选的节点广场与指标详情；
- 后台运行总览、节点管理、分组管理、节点接入、任务中心、系统设置。
- 单管理员账号登录与 CLI 凭据重置。
- 持久会话、CSRF、同源校验、登录限流、审计与自动清理。
- 可选原生 TLS、Agent 安装包校验与 `doctor` 自检。

### Phase 2：规模化增强

- 多账号 RBAC 与审计查询界面；
- Agent 凭据吊销与主动轮换；
- gRPC 双向流的 TLS/mTLS、任务签名、断线续传与持久化结果 outbox；
- 任务失败重试、按节点或分组的定向下发、并发度与脚本签名；
- MTS 原生降采样与 retention 任务；
- SSE 实时更新；
- OpenAPI 生成前端类型与自动化测试。

### Phase 3：规模化

- PostgreSQL 元数据存储；
- 多 Server 横向扩展；
- 分区、归档、对象存储；
- 多租户与审计；
- 可选的 Prometheus 兼容导出接口。

## 10. 当前实现说明

- MTS 使用版本：`v0.0.0-20260813154653-9f14af6435e4`。
- 指标写入使用 `WritePointsAsTypedBatch`，查询使用 `Builder + QueryRows`。
- MTS 通过 `internal/platform/tsdb.Backend` 抽象，后续可替换或增加其他 TSDB。
- 前端为 React 19 + TypeScript + Tailwind + TanStack Router/Query，组件遵循 shadcn 风格。
- Demo 数据与真实 Agent 走同一套写入、SQLite 和查询路径；生产启动默认不启用 Demo。
- 任务中心不再内置预制任务，仅保留管理员创建的任务；gRPC 双向流承载指标上报、即时任务下发、并发执行、结果回执和执行历史。

