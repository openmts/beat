# 当前工作区预览

验证日期：2026-10-20。8081 预览实例当前运行 v232 包（`/readyz` 上报 `0.2.0-dev+worktree.20261020.9`），前端入口 `assets/index-Dk5_raqh.js`；本文件下方按时间倒序记录各批次部署与验证。

- 地址：http://192.168.1.48:8081；后台 `/admin`；指标 `/admin/metrics`。
- systemd：`beat-preview-server.service`、`beat-preview-agent.service`（正在运行，未设置开机自启）。
- 可执行程序及前端：`/opt/beat-preview/current`。
- 当前发布目录：`/opt/beat-preview/releases/worktree-v232-20261020T034140Z`；升级包 `release/preview-v232-20261020/`；上一版目录 `worktree-v231-20261020T033558Z`。
- 独立数据：`/var/lib/beat-preview/data`；schema v30 `accelerator_public_visibility`；SQLite integrity_check=ok、外键 0 违规。
- Agent 配置：`/var/lib/beat-preview/agent-home/agent.json`，由专用 beat-preview 用户运行，每5秒上报。
- 登录资料：`/etc/beat-preview/access.json`，root 私有文件，不入库。
- 已安装全部十套主题（Canvas、Operations、Slate、Aurora、Graphite、Paper、Terminal、Midnight、Solar、沙箱主题 Pulse），当前启用 Slate；已创建“当前主机实时监控”六面板仪表盘，可在指标工作台下拉选择。
- 浏览器实测公共首页和指标工作台正常渲染；Agent 在线、telemetry=fresh。MTS readiness 异常已修复，维护接口返回健康且无分片整理告警。
- A16 浸泡：v226 需要重启后端，因此窗口继续停止，
  未生成达标报告；日志归档为 `/var/tmp/beat-soak/a16-24h.pre-v224-20261019T174000Z.log`。
  新窗口尚未启动，后续需要从 v226 启动时间重新计时。
- 8080 宿主实例与数据未改动。本预览不是完整商用验收；远程操作仍默认关闭。
- v221（2026-10-19）是**只加测试**的一批：把 10 个按包覆盖率低于 90% 的包全部补到 ≥90%，产品代码、前端、
  API 与 schema 都没有改动，因此 8081 未重新部署（`ActiveEnterTimestamp` 仍是 2026-10-19 00:43:32、
  `NRestarts=0`），A16 浸泡窗口不中断；逐包数字与门禁见 `commercial-delivery-plan.md` 的 v221 章节。
- v221 门禁（2026-10-19 实测）：`bash scripts/check.sh backend` **exit=0**——`-race` + `-shuffle` 63 包全 `ok`/0 FAIL、
  `go tool cover -func` 总行覆盖 **92.6%** 且 63 个包全部 ≥90%、`golangci-lint` 0 issues、`go vet` 干净、
  `govulncheck` 0 可达漏洞、双端 `-trimpath` 构建通过；本批无前端改动，前端门禁沿用 v219 结果。

## v232：Agent 版本与灰度升级入口易用性优化（2026-10-20）

- 侧边栏改为“节点与接入”“监控与设置”“高级运维”三组；“Agent 版本与灰度升级”移入高级运维，并与新节点接入入口明确分开。
- 页面顶部用两张引导卡区分“加入新节点”和“升级已有节点”。未启用签名发布模块时不显示版本包、升级任务和平台支持标签，只显示能力影响说明与折叠的技术状态，并明确“不影响节点接入、已有 Agent 上报或监控”。
- 启用模块后将专业术语改为“版本包 / 升级任务 / 平台支持”；原签名导入、分批升级、暂停、回滚、权限控制和五语言支持保持不变。
- 手机端发现并修复标签被裁切的问题：三个标签改为等宽三列并允许两行文本，390px 宽度下三项均完整可见。
- 验证：4 个定向前端文件 / 25 例通过；TypeScript 通过；oxlint 0 error（保留仓库既有 warning）；生产构建通过，API 与 schema 未变化。
- 浏览器实测：1440px 桌面和 390px 手机均无横向溢出、页面错误或失败请求；导航三组、节点接入链接、未启用状态和手机侧栏打开均正常。
- 8081：v232 `0.2.0-dev+worktree.20261020.9`，入口 `assets/index-Dk5_raqh.js`。server/agent 均 active，`/readyz` 全 ok，schema 30、integrity ok、外键 0 违规。
- 部署：冷备份 `/var/backups/beat-preview/pre-v232-20261020T034140Z`，发布目录 `/opt/beat-preview/releases/worktree-v232-20261020T034140Z`。

## v230：首页统计压缩与高级筛选折叠（2026-10-20）

- 方案：搜索、分组、标签、匹配数量和卡片/表格切换保持常驻；在线状态、采样新鲜度、国家、到期、排序、方向与离线排序收进“更多筛选”。高级条件生效后，折叠按钮显示已启用数量。
- 统计摘要改为紧凑的标签/数值行；桌面端与实时连接状态同排，手机端使用两列短行，避免原先低密度大块占用。
- 实测尺寸：1440px 桌面端统计区高 36px、默认筛选区高 58px；390px 手机端统计区高 84px、默认筛选区高 130px。展开高级筛选后仍无横向溢出。
- 五语言文案同步；高级筛选项默认不渲染，展开/收起、活动条件计数和原有筛选功能通过无障碍交互测试。
- 验证：公共页面 2 个定向测试文件 / 6 例、TypeScript、oxlint（0 error，保留既有 warning）和生产构建通过；公共首屏 `249,922 / 256,000 B`。
- 8081：v230 `0.2.0-dev+worktree.20261020.7`，入口 `assets/index-DJwEhJ_l.js`；Playwright 实测桌面/手机展开与折叠、活动筛选计数、卡片/表格切换正常，运行时错误与失败请求均为 0。
- 部署：冷备份 `/var/backups/beat-preview/pre-v230-20261020T012321Z`，发布目录 `/opt/beat-preview/releases/worktree-v230-20261020T012321Z`。server/agent 均 active，`/readyz` 全 ok，schema 30、integrity ok、外键 0 违规。

## v229：首页卡片与表格左右留白修复（2026-10-20）

- 现象：公共首页的卡片和表格条目紧贴主题内容背景左右边缘，分组标题也缺少统一留白。
- 根因：公共结果容器 `#fleet-results` 没有水平内边距，卡片、表格与主题化列表都继承到容器最边缘。
- 修复：在共享结果容器上增加响应式水平内边距：手机 12px、平板 20px、桌面 24px；卡片、表格、主题化列表和分组标题使用同一内容边界。
- 浏览器实测：1440px 桌面端容器内左右各留 24px（内容 `x=128 / right=1312`），390px 手机端各留 12px（内容 `x=28 / right=362`）；卡片/表格切换正常，无横向溢出、运行时错误或失败请求。
- 验证：公共页面 2 个定向测试文件 / 6 例通过；TypeScript、oxlint（0 error，保留既有 warning）和生产构建通过，公共首屏 `249,377 / 256,000 B`。
- 8081：v229 `0.2.0-dev+worktree.20261020.6`，入口 `assets/index-CTIiTIVL.js`；冷备份 `/var/backups/beat-preview/pre-v229-20261020T010340Z`，发布目录 `/opt/beat-preview/releases/worktree-v229-20261020T010340Z`。server/agent 均 active，`/readyz` 全 ok，schema 30、integrity ok、外键 0 违规。

## v228：移除公共大屏功能（2026-10-20）

- 决策：大屏的布局和交互未达到产品标准，Owner 明确要求下线，不再继续修补大屏展示模式。
- 移除范围：首页大屏入口、`/wallboard` 路由与页面、分页/轮换 Hook、大屏参数状态、配置与全屏控件、五种语言中的大屏文案、PWA 大屏导航预缓存以及前端大屏遥测路径映射。普通首页卡片/表格监控、筛选和节点详情保持不变。
- 兼容边界：后端历史遥测枚举和既有数据库记录保留，避免仅为下线展示功能引入 schema 迁移；新前端构建不再识别或上报大屏事件。
- 验证：8 个定向测试文件 / 28 例通过；`tsc -b`、`oxlint`（0 error，保留仓库既有 warning）和生产构建通过。公共首屏降至 `235,107 + 14,262 = 249,369 B / 256,000 B`，构建产物中无 wallboard 分片。
- 8081 真浏览器复测：首页大屏链接数为 0；直接访问 `/wallboard?q=Node` 显示应用内 `404`，无配置、全屏或轮换控件，页面运行时错误为 0。
- 部署：v228 `0.2.0-dev+worktree.20261020.5`，入口 `assets/index-BxxAIotm.js`；冷备份 `/var/backups/beat-preview/pre-v228-20261020T005705Z`，发布目录 `/opt/beat-preview/releases/worktree-v228-20261020T005705Z`。server/agent 均 active，`/readyz` 全 ok，schema 30，integrity ok，外键 0 违规。

## v227：大屏退出与全屏双向切换（2026-10-20）

- 现象：从首页进入 `/wallboard` 后没有可发现的退出入口；原「全屏」按钮只会进入浏览器全屏，再次点击不能稳定切回普通监控。
- 修复：大屏顶部增加可见的「首页」入口，返回 `/` 时保留搜索、分组、标签与排序等首页筛选参数；「全屏」按钮改为进入/退出双向切换，并通过 `fullscreenchange` 同步浏览器 Esc 或系统退出后的按钮状态。离开大屏路由时自动退出残留全屏。
- 验证：页面与语言目录 4 个定向测试文件 / 29 例通过；`tsc -b` 与 `oxlint`（0 error，保留仓库既有 warning）通过；生产构建通过公共首屏硬预算，实测 `241,537 + 14,459 = 255,996 B / 256,000 B`。
- 8081 真浏览器复测：首页点击大屏成功进入 `/wallboard`，点击「首页」返回 `/`；带 `q=Node` 的大屏返回 `/?q=Node`，筛选保留；全屏进入、按钮退出、浏览器外部退出后的 `aria-pressed=false` 同步均通过，页面运行时错误为 0。
- 部署：v227 `0.2.0-dev+worktree.20261020.4`，入口 `assets/index-Cpkxyq5H.js`；冷备份 `/var/backups/beat-preview/pre-v227-20261020T004907Z`，发布目录 `/opt/beat-preview/releases/worktree-v227-20261020T004907Z`。server/agent 均 active，`/readyz` 全 ok，schema 30，integrity ok，外键 0 违规。

## v226：局域网 HTTP 下新增指标面板修复（2026-10-20）

- 现象：8081 通过局域网 HTTP 地址访问时，指标工作区「添加面板」点击无响应；后台已创建并绑定的自定义指标虽然进入管理目录，但无法打开面板编辑器选择它们。
- 根因：页面处于非安全上下文，`window.isSecureContext=false` 且浏览器没有 `crypto.randomUUID`。新增面板、复制/粘贴面板、面板库插入和看板变量编辑直接调用该方法，事件处理器抛出 `crypto.randomUUID is not a function`，React 因而不显示对话框。
- 修复：新增 `webui/src/lib/random-id.ts`，优先使用原生 `randomUUID`，不可用时通过 `crypto.getRandomValues` 生成标准 UUID v4；指标工作区内所有直接调用点改为共享实现。
- 验证：4 个定向测试文件 / 21 例通过；`tsc -b`、`oxlint`（0 error，保留仓库既有 warning）和生产构建通过，入口 `assets/index-80qIeyd-.js`。
- 8081 真浏览器复测：在 `secureContext=false`、原生 `randomUUID=undefined` 的环境下，点击「添加面板」成功打开编辑器；当前两个自定义指标 `load_avg`、`uptime_seconds` 均出现在指标下拉项，页面无运行时错误。两者在当前节点的管理查询均为 HTTP 200 / `state=ok`，各返回 120 个点。
- 部署：`release/preview-v226-20261020`，冷备份 `/var/backups/beat-preview/pre-v226-20261019T231822Z`，原子切换到 `worktree-v226-20261019T231822Z`；server/agent 均 active，`/readyz` 全 ok，schema 30、integrity ok、外键 0 违规。

## v225：GPU 与加速全局/节点可见性（2026-10-20）

- 站点配置新增 `show_accelerators` 总开关，默认开启且优先级最高；关闭后指标页不渲染任何节点的 GPU 与加速区域，公共 REST 也清空设备数据。
- `/admin/accelerators` 新增节点级展示开关，节点无覆盖记录时默认开启；全局关闭时保留节点设置，并在全局恢复后立即生效。
- SQLite v30 增加 `site_settings.show_accelerators` 与 `node_accelerator_visibility`；节点展示开关写入独立审计事件。
- 公共快照路径按当前全局开关校验代际，开关变化时绕过旧快照，避免首页节点列表在缓存窗口内继续显示 GPU 区域。
- 验证：前端 7 个相关文件 / 44 例通过，后端 `go test ./...`、`golangci-lint` 0 issues、`go vet`、`go build ./...` 通过；生产前端构建通过。
- 部署：`release/preview-v225-20261020` 冷备份 `/var/backups/beat-preview/pre-v225-20261019T174326Z`，原子切换到 `worktree-v225-20261019T174326Z`，schema 30 / integrity ok / 外键 0 违规 / `/readyz` 全 ok。
- 实机 API：全局关闭后公共节点的 `accelerator_visible=false`、设备数为 0；节点级关闭后管理读取 `public_visible=false`；恢复后两者均为 `true`。

## v223：站点配置保存 400 修复与前端-only 发布（2026-10-19）

- 现象：后台「站点配置」的任何保存都返回 400 `invalid request body`，因此「展示网络质量」开关无法真正关闭首页网络质量区域。
- 根因：GET `/api/v1/settings/site` 返回只读扩展 `public_telemetry`；表单把它和可编辑字段一起 PUT，后端 `ParseJSONStrict` 拒绝未知键。
- 修复：`webui/src/lib/api.ts` 的 `updateSiteSettings` 改为白名单只提交可编辑字段（`public_telemetry` 不再出站）；新增回归用例断言 PUT 体不含该键。
- 验证：定向 `vitest`（api / settings / pages）全过、`tsc -b` 干净、`oxlint` 0、`npm run build` 通过；完整前端门禁 `bash scripts/check.sh frontend` **exit=0**（255 文件两轮、行覆盖 **90.17%**、`oxlint` 0 error、三套主题浏览器 smoke 全 PASS，日志 `/var/tmp/beat-cov/frontend-gate-v223.log`）；8081 浏览器实机：开关存在，关闭后保存 200，首页 `#network-quality-title` 计数 0，再打开恢复为 1。
- 部署：前端-only，静态目录按请求经 `/opt/beat-preview/current/static` 解析，原子切链到 `worktree-v223-20261019T152711Z`，入口 `assets/index-h10BM_8O.js`；服务端与 Agent 进程未重启（`NRestarts=0`、`ActiveEnterTimestamp` 不变），A16 浸泡窗口继续保持 2026-10-19 11:40:50 起算。回滚只需把 `current` 指回 `worktree-v222-20261019T033953Z`。

## v222：MTS 压缩退化自愈与 8081 部署（2026-10-19）

- 新增按需自愈：维护调度器每 15 分钟读一次存储健康原因，出现 `compaction degraded on shard ...` 时取独占操作锁执行
  `flush + 整库整理`（不删保留窗口内数据、不改就绪口径、不新增 API/schema）；成功记 WARN `repaired degraded MTS compaction`。
  实现见 `internal/store/mts_repair.go`、`internal/maintenance/service.go`、`internal/maintenance/scheduler.go`。
- 门禁：`bash scripts/check.sh backend` **exit=0**（日志 `/var/tmp/beat-cov/backend-gate-v222b.log`）——race 63 包全 ok、
  总行覆盖 **92.6%**、63 包全部 ≥90%、lint/vet 0 issues、govulncheck 0 可达漏洞、双端构建通过。
  首轮门禁在 `internal/store` 既有并发用例上报 5s 超时（那一轮我并行跑了覆盖率与构建），清干净复跑即全绿。
- 部署：8081 升级到 `release/preview-v222-20261019`（发布目录 `/opt/beat-preview/releases/worktree-v222-20261019T033953Z`，
  冷备份 `/var/backups/beat-preview/pre-v222-20261019T033953Z`），入口仍是 `assets/index-C4OyBcNi.js`，
  schema 29、`integrity ok`、外键 0 违规、`/readyz` 24 项全 ok；`maintenance` worker healthy（900s 周期），
  `storage.mts_healthy=true`；新二进制含自愈路径（旧版不含）。
- A16 24h 浸泡窗口随部署重开：2026-10-19 11:40:50 起算，报告 `/var/tmp/beat-soak/a16-24h.json`，
  预计 2026-10-20 11:55 前后出结果；上一窗口 14,756 秒采样（`ready=200`、`failures=0`）归档为
  `/var/tmp/beat-soak/a16-24h.pre-v222-20261019T033943Z.log`。

## v220：8081 MTS 压缩退化的在线修复（2026-10-19）

- 04:44 起 8081 的 `/readyz` 持续 503、到 07:28 累计 641 次失败，而报告/查询/节点/运行时接口全部 200。
- 根因：2026-10-18 20:00 UTC 活跃分片上两个 part 键区间重叠（04:22、04:44 各写一个），flush 期整理被在读引用挡掉后没有重试，只能等下一次每日维护或重启；`metrics_fanout` 的 1950 次
  `snapshot build failed` 是同一根因的下游症状。
- 修复：对 8081 调 `POST /api/v1/settings/maintenance/run`（受守卫的既有维护流程，等同每日自动维护提前触发）。该分片 2 个 part 合成 1 个，`mts_healthy` 转真、`/readyz` 24 项全 ok、`metrics_fanout` 当轮恢复；
  服务端未重启（`NRestarts=0`）、未重新部署、未删除保留窗口内数据。完整证据见
  `commercial-delivery-plan.md` 的 v220 章节。

## v219：指标工作区前端模块拆分与两个包覆盖率补齐（2026-10-19）

- 前端两个超 300 行的文件按依赖分层拆成小模块，函数体一行未改：
  - `src/lib/workspace-portable.ts`（914 行）→ 入口只留 `exportDashboardJSON`/`importDashboardJSON`/`workspaceShareLink`（34 行），
    导入逻辑按「原语 → 规则 → 面板字段 → 面板附加项 → 面板装配」拆成 `workspace-import-core.ts`（188）、
    `workspace-import-rules.ts`（207）、`workspace-import-panel-fields.ts`（190）、
    `workspace-import-panel-extras.ts`（142）、`workspace-import-panels.ts`（181）。
  - `src/components/metric-workspace/panel-options.tsx`（517 行）→ 入口 124 行，另拆出 `panel-axis-options.tsx`（116）、
    `panel-transformation-options.tsx`（170）、`panel-override-options.tsx`（133）。
- 前端验证：`npx tsc -b --noEmit` 无诊断；`npm test -- --maxWorkers=1` **255 文件 / 1547 例全绿**；
  `npm run lint`（oxlint）无 error；`npm run build` 通过，入口分片变为 `assets/index-C4OyBcNi.js`。
- 后端覆盖率补齐（只加测试，不动产品代码）：
  - `cmd/agent` **88.0% → 94.4%**（新增 `runtime_faults_v219_test.go`：enroll 参数与秘密来源校验、
    诊断运行时装配失败、可选运行时收尾、带查询串地址被运营客户端拒绝）；剩余未覆盖是 5 条
    需要子进程或不可达分支（`main.go` 的 `os.Exit`、`acceleratorcollect` 收集失败、上报/通道构造）。
  - `internal/metricserasure` **86.4% → 94.9%**（新增 `worker_faults_v219_test.go`：用 SQLite 触发器注入
    账本迁移失败，覆盖删除/压缩/收尾三个阶段、关闭期间的删除取消、已关闭会话、运行期 panic 归一化）。
- 部署：重建 8081 预览包 `worktree-v219-20261018T164326Z`（v194 包冷备份在
  `/var/backups/beat-preview/pre-v219-20261018T164326Z`），入口 `assets/index-C4OyBcNi.js`，
  schema 29 不变、integrity ok、外键 0 违规；A16 24h 浸泡窗口按部署时间重开。
- 完整门禁（本批实测，2026-10-19）：
  - `bash scripts/check.sh backend` **exit=0**（63 分钟）：`goimports-reviser` 无差异、`go mod tidy -diff`/`verify` 通过、
    `go test -race -shuffle=on -p 2 -timeout 20m ./...` 62 包全绿、`go test -coverprofile ./...` 全绿且
    `go tool cover -func` 总行覆盖 **92.3%**、`golangci-lint` 0 issues、`go vet` 干净、`govulncheck`
    0 可达漏洞、双端 `-trimpath` 构建通过。
  - `bash scripts/check.sh frontend` **exit=0**（10 分钟）：`npm audit` 0 漏洞、**255 文件 / 1547 例**通过、
    行覆盖 **90.17%**、`oxlint` 0 error、`vite build` 通过、三套主题浏览器门禁全 PASS。
- 按包统计仍低于 90% 的 10 个包（本轮未处理，明确记录）：`internal/diagnostics` 86.5%、
  `internal/operatorrecovery` 86.9%、`internal/agentinstall` 88.2%、`internal/sshclient` 87.9%、
  `internal/ingress` 88.4%、`internal/migrationstatus` 88.8%、`cmd/beat-cli` 87.5%、
  `internal/releasesignature` 89.4% 等；它们的剩余条目都是穿插在多步流程里的单条 `return err`，
  要逐层注入依赖缺失或 SQLite 表不可用才能覆盖。仓库级覆盖率要求（≥90%）已满足。

## v218：`internal/store` 主表不可用时的失败关闭覆盖（只加测试，2026-10-19）

- 本批只新增 `internal/store` 的 1 个测试文件 / 3 个用例，该包行覆盖 90.30% → **90.45%**
  （未覆盖语句 1095 → 1078）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -count=1 -shuffle=on` 全绿（39.0 s）。

## v217：`cmd/server` 启动目录与调度就绪守卫覆盖（只加测试，2026-10-19）

- 本批只新增 `cmd/server` 的 1 个测试文件 / 4 个用例，该包行覆盖 93.41% → **94.02%**
  （未覆盖语句 96 → 87）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（cmd/server 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./cmd/server/ -count=1 -shuffle=on` 全绿（18.7 s）。

## v216：`cmd/server` 装配守卫与指标接线失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `cmd/server` 的 2 个测试文件 / 9 个用例，该包行覆盖 91.21% → **93.41%**
  （未覆盖语句 128 → 96）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（cmd/server 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./cmd/server/ -count=1 -shuffle=on` 全绿（18.2 s）。

## v215：`internal/backup` 归档抽取与对账故障注入覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/backup` 的 5 个测试文件 / 16 个用例，该包行覆盖 91.49% → **93.12%**
  （未覆盖语句 209 → 169）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（backup 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/backup/ -count=1 -shuffle=on` 全绿（43.5 s）。

## v214：`internal/backup` 清单/载荷校验与归档条目守卫覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/backup` 的 5 个测试文件 / 17 个用例，该包行覆盖 90.22% → **91.49%**
  （未覆盖语句 240 → 209）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（backup 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/backup/ -count=1 -shuffle=on` 全绿（40.9 s）。

## v213：`internal/agentoperations` agent 侧协议与租约守卫失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/agentoperations` 的 3 个测试文件 / 20 个用例，该包行覆盖 90.03% → **91.71%**
  （未覆盖语句 166 → 138）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（agentoperations 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/agentoperations/ -shuffle=on` 全绿。

## v212：`internal/api` 路由装配与会话守卫失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/api` 主包的 3 个测试文件 / 24 个用例，该包行覆盖 89.76% → **99.58%**
  （未覆盖语句 98 → 4），此前该包低于 90% 门禁线；产品代码与二进制未变化，
  8081 仍为 `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、
  **无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（internal/api 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/api/ -shuffle=on` 全绿（18.9 s）。

## v211：远程操作与网络诊断端点失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/api/handler` 的 3 个测试文件 / 7 个用例（24 个子场景），
  该包行覆盖 93.56% → **94.40%**（未覆盖语句 366 → 318），`remote_operations.go`、`diagnostics_agent.go`、
  `node.go` 已全覆盖；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（api/handler 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/api/handler/ -shuffle=on` 全绿（27.5 s）。

## v210：管理面请求体与协作面依赖缺失失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/api/handler` 的 1 个测试文件 / 6 个用例（30 个子场景），
  该包行覆盖 92.93% → **93.56%**（未覆盖语句 402 → 366）；产品代码与二进制未变化，
  8081 仍为 `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、
  **无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（api/handler 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/api/handler/ -shuffle=on` 全绿（26.5 s）。

## v209：远程操作 agent 通道与租约对账失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `internal/remoteoperations` 的 3 个测试文件 / 13 个用例（36 个子场景），
  该包行覆盖 89.79% → **91.98%**（未覆盖语句 219 → 172），`transfer_agent.go` 已 100%；
  产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、
  **无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（remoteoperations 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/remoteoperations/ -shuffle=on` 全绿（29.9 s）。

## v208：服务端控制面与诊断接线失败关闭覆盖（只加测试，2026-10-18）

- 本批只新增 `cmd/server` 的 5 个测试文件 / 9 个用例（17 个子场景），该包行覆盖 88.32% → **91.21%**
  （未覆盖语句 170 → 128）；产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（cmd/server 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./cmd/server/ -shuffle=on` 全绿（18.1 s）。

## v207：备份包擦除账本与主题归档失败关闭覆盖（只加测试，2026-10-18）

- 收尾从 `internal/store` 转向 `internal/backup`：本批只新增 3 个测试文件 / 8 个用例（24 个子场景），
  该包行覆盖 88.72% → **90.22%**（未覆盖语句 277 → 240）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 窗口不变。
- 本批门禁：`golangci-lint`（backup 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/backup/ -shuffle=on` 全绿（40.6 s）。

## v206：备份上传维护与 MTS 生命周期失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 2 个测试文件 / 7 个用例（17 个子场景），`internal/store` 行覆盖
  90.12% → **90.30%**（未覆盖语句 1116 → 1095）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（39.3 s）。

## v205：加速卡采集与擦除管理面失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 3 个测试文件 / 10 个用例，`internal/store` 行覆盖
  89.81% → **90.12%**（未覆盖语句 1150 → 1116），其中 `metric_erasure_admin.go` 达到 100%；
  产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、
  **无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（39.0 s）。

## 阶段末完整后端门禁（v204 收口后，2026-10-18）

- `bash scripts/check.sh backend` **exit=0**（日志 `/var/tmp/beat-cov/backend-gate-v204.log`）：全仓 goimports 无差异、
  `go mod tidy -diff`/`go mod verify` 干净、`go test -race -shuffle=on -p 2 -timeout 20m ./...` 全包 `ok`（0 FAIL）、
  覆盖门禁 ≥90% 通过、`golangci-lint` 0 issues、`go vet` 干净、`govulncheck` 代码 0 漏洞、双端构建成功。
- 8081 仍运行 `0.2.0-dev+worktree.20261018.127`（本阶段只加测试）；三个单元 active、`/readyz` 200，
  A16 24h 浸泡窗口未被打断。
- `internal/store` 行覆盖自 v195 起从 85.09% 累计提升到 **89.81%**（未覆盖语句 1683 → 1150）。

## v204：入网清理事务失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 1 个测试文件 / 1 个用例（7 个子场景），`internal/store` 行覆盖
  89.73% → **89.81%**（未覆盖语句 1160 → 1150）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（38.6 s）。

## v203：告警规则写入失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 1 个测试文件 / 3 个用例（14 个子场景），`internal/store` 行覆盖
  89.61% → **89.73%**（未覆盖语句 1173 → 1160）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（38.6 s）。

## v202：车队摘要计划与登录阈值账本失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 2 个测试文件 / 11 个用例（19 个子场景），`internal/store` 行覆盖
  89.29% → **89.61%**（未覆盖语句 1209 → 1173）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（38.5 s）。
- 登录阈值裁剪路径用 20001 行种子做了真实回放（裁剪到 20000 上限），未伪造门禁。

## v201：入网授权写入与秘密查找失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 1 个测试文件 / 5 个用例（12 个子场景），`internal/store` 行覆盖
  89.14% → **89.29%**（未覆盖语句 1226 → 1209）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（38.0 s）；首轮 shuffle 命中一次已知高负载波动用例
  （`TestAgentPolicyReadsCustomBindingAndBuiltinFromOneTransaction`，v196 已登记），复跑通过。

## v200：入网挑战与认领/恢复失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 2 个测试文件 / 7 个用例（31 个子场景），`internal/store` 行覆盖
  88.83% → **89.14%**（未覆盖语句 1261 → 1226）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（38.0 s）。

## v199：入网决策/请求族与 OIDC 账本失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 3 个测试文件 / 11 个用例（38 个子场景），`internal/store` 行覆盖
  88.42% → **88.83%**（未覆盖语句 1307 → 1261）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go vet` 干净、
  `go test ./internal/store/ -shuffle=on` 全绿（37.3 s）。
- 剩余未覆盖集中在 `RowsAffected` 读取失败与 `rows.Err()` 迭代故障两类不可注入分支。

## v198：备份上传/加速卡策略失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 3 个测试文件 / 26 个用例（44 个子场景），`internal/store` 行覆盖
  87.90% → **88.42%**（未覆盖语句 1366 → 1307）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go test ./internal/store/` 与 `-shuffle=on` 全绿（37.3 s）。

## v197：擦除/校准/启动探针失败关闭覆盖（只加测试，2026-10-18）

- 沿用收尾方式：本批只新增 `internal/store` 的 4 个测试文件 / 24 个用例（38 个子场景），`internal/store` 行覆盖
  87.30% → **87.90%**（未覆盖语句 1436 → 1366）；产品代码与二进制未变化，8081 仍为
  `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go test ./internal/store/` 与 `-shuffle=on` 全绿。
- 另确认：`/admin/enrollment` 首屏 428（`/admin/agent-enrollment/grants`）是设计内的近期重认证门禁，前端会就地弹窗并重放，
  不是缺陷。

## v195：`internal/store` 覆盖率攻坚（只加测试，2026-10-18）

- 本批只新增 `internal/store` 的 14 个测试文件 / 48 个用例（全仓库行覆盖 48226/53250 = **90.57%**，`internal/store` 85.09% → 86.68%），
  生产代码、前端 bundle 与 Agent 二进制均未变化，8081 **无需重新部署**：实例仍为 `0.2.0-dev+worktree.20261018.127`、
  入口 `assets/index-BC7JL2Np.js`、schema 29。
- A16 24h 浸泡窗口保持不打断（2026-10-18 18:06 起算，预计 2026-10-19 18:22 出结果）；若重部署会重开窗口，故本轮不重启。
- 门禁：`bash scripts/check.sh backend` exit=0（race / coverage / lint / vet / govulncheck / 双端构建），
  日志 `/var/tmp/beat-cov/backend-gate-v195.log`，明细 `/var/tmp/beat-cov/repo-v195.out`。

## v196：告警评估与车队摘要簇的失败关闭覆盖（只加测试，2026-10-18）

- 沿用 v195 的收尾方式：本批只新增 `internal/store` 的 5 个测试文件 / 26 个用例，`internal/store` 行覆盖 86.68% → **87.30%**；
  产品代码与二进制未变化，8081 仍为 `0.2.0-dev+worktree.20261018.127`、入口 `assets/index-BC7JL2Np.js`、**无需重新部署**，
  A16 24h 浸泡窗口保持不变。
- 本批门禁：`golangci-lint`（store 包）0 issues、`goimports-reviser` 无差异、`go test ./internal/store/` 与 `-shuffle=on` 全绿。

## v194：Grafana 对齐收口与 `soak.py` 门禁（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.127`，发布目录 `/opt/beat-preview/releases/worktree-v194-20261018T100529Z`，
  冷备份 `/var/backups/beat-preview/pre-v194-20261018T100529Z`（记录 v189，可 rollback），入口
  `assets/index-BC7JL2Np.js`，schema 仍 29、`integrity ok`、外键 0 违规。
- 所有者「按建议全批准」后落地的三处 Grafana 对齐细则（详见 `commercial-delivery-plan.md` 的 v194 章节）：
  覆盖匹配器补「按字段类型」（单位族枚举）与「按值」（最新样本等于给定数值）；覆盖补逐序列
  Display name / Decimals / Min / Max；面板级补 Standard options → No value 占位文本。
- 编辑器：匹配值控件随匹配方式切换（单位族下拉 / 数字输入），Standard options 与「空值文本」按面板类型出现；
  JSON 导入导出白名单补 12 个键，五语言目录各补 16 个键。
- 宿主冒烟（`/var/tmp/beat-smoke-v194.py`）：带新字段的 gauge 看板 `POST` → 201；7 条负例（非法单位族、
  非数值 `by_value`、小数位 7、值域倒置、显示名 129 B、`table` 带值域、`timeseries` 带 `no_value`）全部 **400**；
  `DELETE ...?revision=` → **204**。
- 门禁：`bash scripts/check.sh backend` exit=0（race / coverage / lint / vet / govulncheck / 双端构建），
  `bash scripts/check.sh frontend` exit=0（255 文件 / 1547 用例两轮通过、行覆盖 90.17%、三套主题 smoke PASS），
  `python3 scripts/tests/run.py` 64 例全绿且 `soak.py` 行覆盖 97.82% 进默认门禁。
- 本批部署重启了预览实例，A16 24h 浸泡窗口自 2026-10-18 18:06 重开（旧窗口采样已归档）。

## v193：验收登记册逐行复核（只改文档与证据，2026-10-18）

- 内容：C01–C16 逐行复核后闭合（保留原有证据明细，State 改以「已闭合（2026-10-18 v193 复核：…）」开头），
  C17 只剩 A16 的 24h 浸泡结果；执行顺序表 Phase 1–4 标 Completed、Phase 5 In progress。
- 本轮新证据：后端/前端/脚本门禁全绿（见 v191、v192），容器门禁用 podman 垫片实跑 exit=0
  （镜像里跑完 `npm ci` + `vite build`，容器 uid 65532、`/readyz` 就绪）；顺带清理本机遗留的
  14 个 buildah working container 与 3 个悬挂层约 4 GB。
- 同时修掉 v149/v158/v162/v163 里已被后续批次做掉的「仍未做」表述（覆盖 fill below to/阈值/轴位置、
  `axis_width`、`filter_by_value` 的 `column`、`sort_by` 的 `field`、声明摘要与市场目录 `configuration`）。
- 产品代码、前端 bundle 与 Agent 二进制均未变化，8081 仍运行 `0.2.0-dev+worktree.20261018.126`，**无需重新部署**。

## v192：B4 主题浏览器门禁进默认门禁（2026-10-18）

- 内容：`webui` 新增 devDependency `playwright@1.62.0`，`scripts/check.sh frontend` 构建后自动装 headless shell
  （`--only-shell`）并跑三套主题浏览器 smoke（declarative / sandbox / market）；CI 前端 job 加
  `playwright install-deps chromium`。浏览器装进仓库专用目录 `~/.cache/beat-playwright`，
  宿主不再需要手工提供 Playwright 包与 Chromium 路径。
- 实跑：`bash scripts/check.sh frontend` 全绿——`npm audit` 0 漏洞、255 个测试文件通过（单测 + 覆盖率两轮）、
  行覆盖 90.13%、`oxlint` 0 error、构建入口仍是 `assets/index-DAP-fw2A.js`、三套浏览器门禁全 PASS（单跑约 47 秒）。
- 产品代码、前端 bundle 与 Agent 二进制均未变化，8081 仍运行 `0.2.0-dev+worktree.20261018.126`，**无需重新部署**。

## v191：覆盖率门禁恢复（只改测试，2026-10-18）

- 完整 backend 门禁（race + coverage + lint + vet + govulncheck + 双端构建）随后实跑通过：exit=0、无 FAIL。
  教训：用 `systemd-run` 跑门禁必须显式传 `HOME`，否则 `internal/agent` 的
  `TestSelfHealUsesHomeDirWhenEmpty` 会以 `$HOME is not defined` 假失败（不是代码回归）。
- 现象：A 批（v183–v190）落地时只跑了 `go test ./...`，没跑 `scripts/check.sh backend` 的覆盖率判定，
  后端口径实测已跌到 **89.75%**（47763/53216），低于 90% 硬阈值。根因是 A 批新增的
  `internal/retentionadmin/service.go`、`internal/remoteoperations/transfer_discard.go`、
  `internal/api/handler/agent_retention.go`、`internal/store/agent_retention_settings.go`、
  `internal/model/theme_configuration.go` 五个文件一行都没被覆盖。
- 处置：新补 10 个测试文件 / 40 个用例（保留期三层 + 草稿丢弃九类负例 + 主题配置键推导 + MTS 加速卡归档校验 +
  擦除指标 + 节点生命周期/GeoIP 路由表 + Agent 上报准入），总覆盖率回到 **48018/53216 = 90.232%**，全部包 `ok`、0 FAIL。
- 同时修掉两个只在本仓库门禁下必现的假失败：`scripts/check.sh` 的 `umask 077` 让
  `internal/operatorrecovery` 与 `internal/store` 的「世界可读」用例写出的文件其实是 0600，两处补显式 `Chmod`；
  `umask 077` + `-shuffle=on` 全量测试复跑全绿。
- 生产代码、前端与 Agent 二进制均未变动，8081 仍运行 `0.2.0-dev+worktree.20261018.126`，**无需重新部署**；
  验收方式与上一批一致。

## v189：A12 `beat-cli` 管理面会话凭据（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.126`，发布目录 `/opt/beat-preview/releases/worktree-v189-20261018T044903Z`，
  冷备份 `/var/backups/beat-preview/pre-v189-20261018T044903Z`（记录 v188，可 rollback）。
- `beat-cli` 新命令：`erasure list|get|create|retry|cancel`（owner 专属擦除）与 `operations tasks list|cancel`
  （远端任务清理）；凭据用 `--admin-user/--admin-password-file/--admin-totp-code/--session-file`
  （环境变量 `BEAT_ADMIN_USER`、`BEAT_ADMIN_PASSWORD`、`BEAT_ADMIN_TOTP`），口令与会话文件都必须 0600。
- 服务端鉴权一字未改：仍在 `owner(...)`/`router.admin` 上，CLI 只是登录后台会话并补一次最近认证。
- 宿主验证（部署后的 `/opt/beat-preview/current/beat-cli`）：
  `--admin-user preview --admin-password-file <0600> --session-file <0600> erasure list --limit 2` 与
  `operations tasks list --limit 2` 返回真实数据；错误口令 `invalid credentials (HTTP 401)`、缺口令本地报错。

## v188：A9 官方安装器参数化（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.125`，发布目录 `/opt/beat-preview/releases/worktree-v188-20261018T044018Z`，
  冷备份 `/var/backups/beat-preview/pre-v188-20261018T044018Z`（记录 v187，可 rollback）。
- `scripts/beatctl.sh` 支持 `BEAT_LISTEN_ADDR`（缺省 `:8080`）与 `BEAT_SERVICE_ACCOUNT`（缺省 `beat`），
  与既有目录变量一起构成安装面；缺省值生成的单元与升级前逐字节一致，install/upgrade/uninstall 共用同一组变量。
- 非法账号名与非法监听地址在任何改机动作之前就被拒绝；就绪探针固定回环地址（`:8080`、`0.0.0.0:8080`、
  `[::]:8080`、`*:8080` 都探 `127.0.0.1`）。
- 验证：`python3 scripts/tests/run.py` 46 例全绿，`beatctl.sh` 行覆盖 97.39%；8081 本批无服务端行为变更，
  同步版本仅为可见性（`/readyz` 报告版本且 24 项 check 全 ok）。

## v187：A8 发布对象保留期后台可配置（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.124`，发布目录 `/opt/beat-preview/releases/worktree-v187-20261018T042954Z`，
  冷备份 `/var/backups/beat-preview/pre-v187-20261018T042954Z`（记录 v186，可 rollback）。
- schema 28 → **29**（`agent_release_retention`）：后台可配置 rollout / release 保留天数（上限 3650，
  0 表示保留全部历史），未保存配置时沿用启动环境变量缺省，升级后行为不变；清理 worker 只在至少一项
  生效时注册，结果回写设置行供后台展示。
- 接口：`GET/PUT/DELETE /api/v1/settings/agent-release-retention`、`POST .../run`（均 owner/admin）。
  非法天数 400、无生效保留期手动清理 409、内部故障 503。
- 管理端：Agent 发布页新增「Release retention」面板（改天数、恢复服务端缺省、立即清理 + 最近一轮结果）。
- 宿主验证：`/tmp/beat-check/v187-a8-smoke.py` **19/19 通过**（含 400 与 409 负例、0/0 语义、
  恢复缺省后保留最近一轮记录）；Playwright 复核面板渲染无页面错误，截图
  `/tmp/beat-check/v187-agent-retention.png`。
- 说明：跑后端测试时 `TMPDIR` 用 `/root/.cache/beat-tmp` 会让 `internal/operatorrecovery` 的
  rootless 子进程 exec 失败（`/root` 对 nobody 不可穿越），该包用 `TMPDIR=/var/tmp` 即通过。

## v186：A10/A11/A20/A21/A22/A24 + `available_from` 缓存（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.123`，发布目录 `/opt/beat-preview/releases/worktree-v186-20261018T041452Z`，
  冷备份 `/var/backups/beat-preview/pre-v186-20261018T041452Z`（记录 v185，可 rollback）。v185（`.121`）是
  同批次的首次部署，宿主演练暴露 A10 删除顺序被外键挡住后由本版修正，v185 视为被取代。
- 用户 2026-10-18「按建议全批准」后落地的 A 类条目（详见 `commercial-delivery-plan.md` 的 v184 章节）：
  - A11 finalize 整体摘要不符由 503 改为 409 并给出「上传字节与声明摘要不符」的原因（`ErrCorrupt`）；
  - A20 指标查询超时（`context.DeadlineExceeded`）单独映射成 429 + `Retry-After: 1` + 「缩短时间范围」；
  - A21 rollout 目标进入终态后原因 first-wins，尝试行的 `ended_at` 不再被后续上报周期刷新；
  - A22 `invalid agent update assignment` 拆成 entrypoint / 协议区间 / 清单缺产物三种原因（HTTP 契约不变）；
  - A24 `GET /api/v1/admin/backups/quarantine` 保留 `items`/`total`，新增只读 `upload_sessions` 数组；
  - A10 新增 `DELETE /api/v1/admin/operations/transfers/{id}` 丢弃从未 finalize 的上传草稿，被在途任务或
    扫描引用时回 409，用于解除草稿对节点删除的占用；管理台传输详情加「丢弃草稿」动作（五语言文案）；
  - `available_from` 边缘探测加 60s TTL 缓存（不改响应契约）。
- 宿主演练（`/tmp/beat-check/v185-drill.py`，临时节点 + 临时 agent，演练后节点、策略、传输行与分块文件全部回收）
  **22/22 断言通过**：隔离清单新增字段；摘要不符 409 且原因可读；草稿存在时 `DELETE /nodes/{id}` 回
  409 `node has active remote operations`，丢弃草稿后节点删除受理并完成；终态（corrupt）记录不能走草稿通道
  （409）；审计 `operation.transfer.discarded` 落库；无残留 transfer target。
- 门禁：`go build ./...`、`go test -count=1`（store/metricquery/agentrollout/agentupdate/remoteoperations/
  backup/api 共 11 包全 ok）、`golangci-lint run` 上述包 0 issues、`goimports-reviser` 通过；
  `tsc -b` 干净；`vitest run`（operation-transfers/operations/context 等 13 文件 63 用例）通过；
  `npm run build` 入口 `assets/index-3ZsukZYU.js`。
- `/readyz` 24 项全 ok、两个单元 active、schema 28 / integrity ok / 外键 0 违规。

## v184：A6/A7 主题市场配置声明（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.120`，发布目录 `/opt/beat-preview/releases/worktree-v184-20261018T035001Z`，
  冷备份 `/var/backups/beat-preview/pre-v184-20261018T035001Z`（记录 v183）。
- 变更：市场目录条目暴露目标包的 `configuration`（`ThemeCatalogEntry.Configuration`，发布与目录校验共用
  `validateManifestConfiguration`）；后台「包声明摘要」从只读升级为可就地改配置，复用既有的
  `PUT /admin/themes/{id}/configuration` 与 428 重认证弹窗；内置/沙箱主题保持只读。
- 实机证据：官方 `beat-theme market publish` 产出的签名目录已带规范化后的 `configuration`
  （`{"type":"managed","items":[…overview/radius/show_tags]}`）；8081 上对已安装的 `beat-slate 1.0.1`
  走预览 → 保存 → 读回 → 越界/未声明键 400 → 重置，13/13 断言通过（`/tmp/beat-check/v184-a7-smoke.py`）。
- 门禁：`go build ./...`、`go test ./internal/theme/... ./internal/model/...` 通过、
  `golangci-lint run ./internal/theme/... ./internal/model/...` 0 issues、`tsc -b` 干净。

## v182：Agent 卸载的安装锁覆盖（2026-10-18）

- 版本 `0.2.0-dev+worktree.20261018.118`，发布目录 `/opt/beat-preview/releases/worktree-v182-20261018T031009Z`，
  冷备份 `/var/backups/beat-preview/pre-v182-20261018T031009Z`（记录上一版 v179，可 rollback）。
- 变更：`beat-agent service uninstall --purge` 此前在删完服务单元后就释放安装锁，清除 `/etc/beat-agent`
  时锁已不在；`join`/`service install`/`update-enable` 共用这把锁，因此并发安装可能被删掉半个配置。
  现在 `Uninstall` 取锁后才进入两个阶段，行为口径不变（无接口/契约/schema 变化）。
- 宿主真机：安装 → 另一进程持锁时卸载报 `another beat-agent installation is running` 且不删任何文件、
  服务仍 active → `uninstall` 保留配置 → `uninstall --purge` 清空 → 未安装态退出码 1；演练产物已清理。
- 验证：`/readyz` 24 项全 ok、两个单元 active、公网 `/readyz` 200、schema 28 / integrity ok / 外键 0 违规；
  指标工作区宿主冒烟在该版本上 17/17 通过（入口仍是 `assets/index-BOhvMXzO.js`）。

## v181：指标工作区冒烟脚本竞态假失败修复（非 8081 部署）（2026-10-18）

- 8081 仍是 v179，本轮无产品代码改动、不重新部署；改动只在 `webui/scripts/metric-workspace-smoke.mjs`。
- 该脚本此前在同一版本上时通时不通（3 跑 2 败），失败的是覆盖项面板的两条断言，而失败截图里
  面板已经画出两条曲线与右轴 `Mem axis`；根因是「等到面板 region 出现就断言」早于查询结果出图。
- 修复：断言前先等四个面板的「上次成功查询」标记，两条覆盖项断言改为有界轮询；修复后连续 4 跑
  全部 `17 checks` 通过。
- 同轮宿主巡检公共面 + 18 个后台页：控制台错误 0、失败响应 0（`/admin/enrollment` 的 428 是设计内
  就地重认证）、损坏图片 0；主题三套浏览器门禁用已部署静态目录全过。

## v180：主题包备份/恢复链路（隔离实例，非 8081 部署）（2026-10-18）

- 8081 上跑的是 v179 包（未变），本轮不改代码、不重新部署；证据来自隔离实例 `127.0.0.1:18097`
  （独立 `data-a`，与 8081/8080 无共享状态）。
- 正向：上传 `slate`+`aurora` → 激活 `aurora` → 建备份（`format_version 4`、10 个 `themes/<digest>/...` 条目）
  → 删掉 `slate` → `stage-restore` 后重启 → 两个包都回来，四个文件逐个 sha256 与归档一致，
  再激活 `slate` 成功（`GET /api/v1/themes/active` = `Slate 1.0.1`）。
- 负例：篡改 `archive.zip`、删掉条目并同步 manifest、删掉活动包整包，三个变体都 `finalize=400`、
  不新增备份记录、上传会话进 `quarantined`；未改动的对照归档正常 `201`。
- 演练暴露的观测缺口登记为 A24（上传会话隔离未进 `/admin/backups/quarantine`）。
- 隔离实例与 `/tmp/beat-check/c09-restore/` 已清理。

## v179：Agent 卸载命令 + 维护调度用例时钟缺陷（2026-10-18）

- 能力：`beat-agent service uninstall [--purge]`。默认停用并删除 `beat-agent.service`、
  `beat-agent-update.service`、`/opt/beat-agent` 下的二进制与更新状态，保留 `/etc/beat-agent` 的配置与身份；
  `--purge` 再删配置与身份文件，目录空了才删目录。非本安装器写的单元、权限不合规的文件或目录、
  `systemctl disable` 失败都会在删除前中止；未安装的机器返回「not installed」并退出码 1。
- 宿主真机（真实 systemd）：install → active → uninstall（配置保留）→ `--purge`（目录删除）→
  再次执行报未安装；证据 `/tmp/beat-check/v179-uninstall/drill.log`，宿主演练产物已清理。
- 门禁发现：`internal/maintenance` 的 `TestSchedulerRunsRepeatedCycles` 用真实时钟，默认清理小时 03:00 UTC，
  每天 00:00–03:00 UTC 必然失败（本轮 02:20 UTC 连续 3 次复现）；改为注入固定时钟后全量通过。
- 门禁：`go test -count=1 ./...` 全绿、总覆盖率 90.2%、`goimports-reviser` + `golangci-lint` 0 issues；
  前端未改动。
- 部署：`release/preview-v179-20261018`，冷备份 `/var/backups/beat-preview/pre-v179-20261018T022326Z`，
  `schema 28 / integrity ok / fk violations 0`、`/readyz` ready、两个单元 active。

## v129：指标工作区读并发闸门（2026-10-17）

- 缺陷：一次看板加载会同时发起目录/看板/节点/分组四路读，再叠加面板查询、注释与面板告警；服务端
  按身份限制在途管理读（`AdminRead` 4、公共历史 2），超出后返回 `{"code":429,"message":"request rejected"}`，
  受影响面板只能显示「request rejected」。v128 实测：3 面板看板 3 次 429、4 面板看板 4 次。
- 修复：新增 `webui/src/lib/workspace-read-gate.ts`，同一标签页里的指标读共享两个并发名额；
  接入 `getWorkspaceSources` 的四路读、`queryWorkspaceMetrics`、`queryWorkspaceLatest`、
  `listAnnotations` 与 `listPanelAlerts`。服务端准入额度未改动。
- 实机（8081）：4 面板看板（percent_change / range / delta / first）一次加载 8 次管理读、0 次失败、
  peak in flight 3、四个面板全部渲染数值（247.99% / 39.39% / 14.18% / 50.64%），页内零错误。
- 门禁：`tsc -b`、定向 vitest 26 例、全量 vitest 238 文件 1387 例、`npm run lint`、`vite build`。

## v128：值面板窗口计算 + 面板数组字段崩溃修复（2026-10-17）

- 能力：值面板（stat / gauge / bargauge / piechart）的计算方式补齐 Grafana 的 First、Range、
  Delta、Percent change：`range` = 最大样本 − 最小样本，`delta` = 最新 − 最早，
  `percent_change` = (最新 − 最早) / |最早| × 100；有效样本不足两个（或百分比基准为 0）时显示
  「无数据」，不伪造 0。未知取值仍按 `panel calculation is invalid` 拒绝，折线与表格仍拒绝该字段。
- 前端：值计算下拉新增四项，导入导出按同一清单校验；五语言文案补齐。
- 缺陷修复：`ValidateDashboard` 现在把 nil 的 `series` / `thresholds` 补成空数组——此前通过 API
  落库的看板会回传 `"thresholds": null`，指标工作区把面板数组当数组用，整个页面直接
  「Page resources could not be loaded」。
- 实机（8081，`/usr/bin/python3` + Playwright）：
  - API 回放 13/13：four 新取值 + gauge/bargauge/piechart 可用，`avg`/`median`/`percent` 与
    timeseries/table 显式声明都被拒。
  - 浏览器：下拉列出十个取值；first=5.83%、range=99.96%、delta=13.67%、percent=274.68% 均正常渲染，
    页内零错误；同一窗口的三面板看板给出 first=5.85%、delta=11.8%、percent=201.54%，
    与 (delta/|first|)×100 = 201.71% 相符（差异来自各面板查询时刻）。
- 已知问题（下一步修复）：多面板看板并发的管理读（面板查询 + 注释 + 面板告警）会超过服务端
  `AdminRead` 每身份 4 个在途请求的准入额度，个别面板短暂显示「request rejected」；3 面板看板实测
  出现 3 次 429，4 面板看板出现 4 次。计划在前端为工作区指标读加共享并发闸门。

## v127：值面板排版方向（Grafana Stat → Orientation）（2026-10-17）

- 能力：`stat_orientation` 取 auto（缺省，数值在上、名称在下）/ horizontal（名称与数值同一行）/
  vertical（名称在上、数值在下）；未知取值按 `stat orientation is invalid` 拒绝，只对 stat 面板生效。
- 前端：Stat styles 新增方向下拉；类型切换一起清；导入导出携带并校验；五语言文案、提示与错误文案补齐。
- 实测（8081）：接口 8 个用例符合预期（省略/auto/horizontal/vertical → 201 且回读一致；
  `diagonal`、数字 → 400；gauge 与 timeseries 带字段 → 400）。
  浏览器演练：auto 数值 top 520 / 名称 top 580（名称在下），horizontal 名称与数值同一行
  （数值 left 683、名称 left 798），vertical 名称 top 520 在数值 top 556 之上，改回 auto 与初始一致；页面零错误。
- 门禁：`goimports-reviser`、`go test ./internal/model/... ./internal/api/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、`npm run lint`（仅既有 warning）、
  全量 vitest 238 文件 1387 例、`vite build` 通过公共外壳 gzip 预算。
- 冷备份 `pre-v127-20261017T062704Z`；入口 `assets/index-BNV4SscD.js`；schema 仍 v28。

## v126：指标擦除缺陷修复（2026-10-17）

- 缺陷一：擦除的封闭清单只认内建测量，含自定义指标（`custom_*`）的实例
  `delete_node`/`fleet_telemetry` 擦除必然 `inventory_mismatch`。修复后清单与指标目录对齐，
  未登记测量仍失败关闭。
- 缺陷二：任务级失败让 `metric_erasure` worker 退出，监督器判 `required background worker terminated`，
  整个服务端重启（实测 14:04:36–14:04:39 约 3 秒不可用）。修复后任务级失败只上报监督器并继续循环。
- 实机：此前失败的擦除任务 `9f6df59e`（目标 `c12 drill node`）在 8081 上 retry 后 `succeeded`，
  节点行删除，`NRestarts=0`，`/readyz` 的 `metric_erasure` 回到 `ok`。
- 门禁：`go build ./...`、`go test ./internal/store/... ./internal/metricserasure/...`、
  `goimports-reviser`、`golangci-lint run ./internal/store/... ./internal/metricserasure/...` 0 issues。
- 冷备份 `pre-v126-20261017T061139Z`；schema 仍 v28。

## v125：值面板 Text size（Grafana Stat → Text size）（2026-10-17）

- 能力：`DashboardPanel` 新增 `stat_text_size`：auto（缺省，沿用 text-4xl）/ sm / md / lg / xl；
  未知取值按 `stat text size is invalid` 拒绝，只对 stat 面板生效，其它面板类型带上一律拒绝。
- 渲染：主文本按档位换 Tailwind 字号，auto 与 lg 保持原有 36px，sm/md/xl 分别是 24/30/48px。
- 前端：Stat styles 新增字号下拉；切换面板类型与另两个 stat 字段一起清掉；导入导出携带并校验；
  五种语言补齐文案、提示与错误文案。
- 实测（8081）：接口 11 个用例符合预期（auto 省略/auto/sm/md/lg/xl 都 201 且回读一致；
  `xxl`、`48px`、gauge、timeseries 带该字段都 400）。
  浏览器演练：auto 与 lg 的主文本 `font-size: 36px`，Small 24px、Medium 30px、Extra large 48px，
  页面无错误。
- 门禁：`goimports-reviser`、`go test ./internal/model/...`、`golangci-lint run ./internal/model/...` 0 issues、
  `tsc -b`、`npm run lint`（仅既有 warning）、vitest（workspace-stat 6 例、workspace-portable 51 例、
  locales / catalog-integrity / context 共 85 例、metric-workspace 组件 22 文件 124 例）、
  `vite build` 通过公共外壳 gzip 预算。

## v124：值面板 Text mode 扩展（Grafana Stat → Text mode）（2026-10-17）

- 能力：`stat_text_mode` 取值扩展为 auto（缺省）/ value / name / none；未知取值仍按
  `stat text mode is invalid` 拒绝，且只对 stat 面板生效，其它面板类型带上一律按
  `stat styles are only available on stat panels` 拒绝。
- 渲染：auto 保持数值 + 系列名 + 采样时间；value 只留数值；name 把系列名顶到主文本（沿用阈值色）；
  none 整块文本都不渲染。面板标题在所有模式下都保留。
- 前端：编辑器「显示内容」下拉自动列出四个选项；导入导出沿用同一份取值表；五种语言补齐文案，
  `workspace.stat_text_mode_invalid` 与 `workspace.stat_style_hint` 同步扩展。
- 实测（8081）：接口 8 个用例符合预期（auto 省略/auto/value/name/none 都 201 且回读一致；
  `labels`、timeseries、gauge 带该字段都 400）。
  浏览器演练切到 name 时主文本变成 `CPU usage · 当前主机 · 实时采集`（阈值绿色），
  none 时面板不再渲染文本，value 时只剩 `7.89%`，页面无错误。
- 门禁：`goimports-reviser`、`go test ./internal/model/...`、`golangci-lint run ./internal/model/...` 0 issues、
  `tsc -b`、vitest（workspace-stat / workspace-portable / locales / catalog-integrity / context 共 83 例）、
  `vite build` 通过公共外壳 gzip 预算。

## v123：百分比堆叠（Grafana Stack → Percent）（2026-10-17）

- 能力：`stack` 取值扩展为 none / normal / percent；percent 把每个时间点按绝对值占比归一化成 100%。
- 百分比堆叠与对数轴互斥（对数轴放不下 0，刻度也失去意义），按 `percent stacking needs a linear axis`
  拒绝；服务端、面板编辑器与看板导入三处同一口径（导入时 stack 与 scale 分两处解析，因此导入后补一次组合校验）。
- 渲染：折线与柱状图共用同一套归一化，坐标轴切到 0..100% 刻度，tooltip 用每个时间点的总量把坐标还原成
  原始单位与原始值；null 保持缺失样本，空时间点总量为 0 不除零；堆叠时按需补上可读填充。
- 前端：堆叠下拉新增「百分比堆叠」；导入导出同步携带并校验；五种语言补齐文案。
- 实测（8081）：接口 7 个用例符合预期（折线/柱状 percent 都 201；percent+log、未知取值、
  stat 带堆叠都 400）。
  浏览器演练 `net_recv` + `net_sent` 双序列：percent 下 Y 轴为 `0% / 30% / 60% / 100%`、两条堆叠面积，
  tooltip 显示原始值 `16.77 KiB/s`、`2.71 KiB/s`；切回「不堆叠」后 Y 轴回到
  `0 … 585.94 KiB/s`；再切回 percent 恢复 0..100%；把刻度改成对数时提示
  `Percent stacking needs a linear axis.` 并阻止 Apply，改回线性后正常，页面无错误。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/...`、`go test ./internal/api/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（238 文件 1383 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,008 B + 最大语言分片 14,333 B = 255,341 B / 256,000 B）。

## v122：柱对齐（Grafana Bar chart → Bar alignment）（2026-10-17）

- 能力：`DashboardPanel` 新增 `bar_alignment`：auto（缺省，等同于 centered）/before（柱子结束于时间点）/
  centered（柱子中心在时间点）/after（柱子起始于时间点）；只对 barchart 面板生效，其它面板类型按
  `bar chart options are only available on bar chart panels` 拒绝，未知取值按 `bar alignment is invalid` 拒绝。
- 渲染：auto 与 centered 保持 Recharts 的默认柱形，现有看板渲染不变；before/after 用自定义柱形把柱子
  整体平移半个柱宽，圆角与渐变填充沿用同一份样式。
- 顺带修复：行/文本面板此前会放过 `bar_width`/`bar_radius`，现在与 `bar_alignment` 一起登记进
  `panelCarriesQueryOptions`，不再能保存出永远不渲染的配置。
- 前端：柱状图选项区新增「柱对齐」下拉，选回 auto 时省略字段；切换可视化类型自动清理；
  看板导入导出同步携带并校验；五种语言补齐文案。
- 实测（8081）：接口 9 个用例符合预期（before/after/centered/auto 都 201 且回读一致；未知取值、
  timeseries、stat、行面板带对齐、行面板带柱宽都 400）。
  浏览器演练 `bar_width=24` 的柱状图：实测柱宽 4px（类目数限制），centered 相对 before 右移 2px、
  after 再右移 2px，正好是半个柱宽；恢复 before 后偏移回到原位，页面无错误。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（238 文件 1383 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,017 B + 最大语言分片 14,333 B = 255,350 B / 256,000 B）。

## v121：右侧图例宽度（Grafana Legend → Width）（2026-10-17）

- 能力：`DashboardPanel` 新增 `legend_width`：右侧图例列的像素宽度（120..480）。只在图例打开、
  位置为 `right`、面板类型为 timeseries/barchart 时有效；其它组合按
  `legend width requires a visible legend` / `legend width is only available on timeseries and bar chart panels` /
  `legend width needs the legend placed on the right` / `legend width must be between 120 and 480` 拒绝。
- 渲染：显式声明时用行内宽度覆盖自带类名；缺省沿用渲染器自带宽度（列表模式 160px、表格模式 224px），
  历史看板渲染不变。
- 前端：图例位置选到右侧后面板编辑器显示宽度输入，并在越界时内联提示；关闭图例或换回底部自动清理该字段；
  切换可视化类型清理；看板导入导出同步携带并校验；五种语言补齐文案。
- 实测（8081）：接口 8 个用例符合预期（右侧显式 300、右侧缺省、柱状 420 都 201 且回读一致；
  底部带宽度、100、500、隐藏图例、stat 面板都 400）。
  浏览器演练「当前主机实时监控」首个面板：图例改到右侧后缺省列宽 160px；填写 300 后实测 `300`；
  填 100 时内联提示 `Legend width must be a whole number between 120 and 480.` 并阻止 Apply，
  改回 300 或清空后提示消失；页面无错误（演练未保存，用户看板定义未被改动）。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（238 文件 1383 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,029 B + 最大语言分片 14,333 B = 255,362 B / 256,000 B）。

## v120：Gauge 阈值刻度与标签（Grafana Gauge → Show threshold markers / labels）（2026-10-17）

- 能力：`DashboardPanel` 新增 `gauge_threshold_markers` 与 `gauge_threshold_labels`，缺省分别等于
  显示刻度与不显示标签（与 Grafana 一致）。两者只对 gauge 面板生效，其它面板类型按
  `gauge threshold markers are only available on gauge panels` 拒绝。
- 依赖关系：标签依附刻度，显式关掉刻度又打开标签按 `gauge threshold labels need threshold markers`
  拒绝；没有阈值时标签无处可标，按 `gauge threshold labels need thresholds` 拒绝。
- 渲染：仪表条上按阈值位置画出刻度（沿用阈值颜色），标签在刻度下方显示阈值数值；
  超出轴范围（`min`..`max`）的阈值自动跳过，避免溢出仪表条。
- 前端：面板编辑器新增「仪表选项」区（两个开关），保存前按同一组规则拦截；切换可视化类型自动清理该字段；
  看板导入导出同步携带并校验；五种语言补齐文案。
- 实测（8081）：接口 8 个用例符合预期（缺省、显式开、显式关、标签+阈值都 201 且回读一致；
  刻度关闭又开标签、标签无阈值、stat 带刻度、barchart 带标签都 400）。
  浏览器演练「v120 Gauge 验收」看板：`min=0`/`max=100`、阈值 50/80 时仪表条上出现 `50%` 与 `80%`
  两处刻度；打开「显示阈值标签」后刻度下方显示 `50%`、`80%`；再关掉刻度时提示
  `Threshold labels need the threshold markers switch turned on.` 并阻止 Apply；恢复后页面无错误。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（238 文件 1383 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,019 B + 最大语言分片 14,333 B = 255,352 B / 256,000 B）。

## v119：零值居中（Grafana Axis → Centered zero）（2026-10-17）

- 能力：`DashboardPanel` 新增 `centered_zero`：坐标轴对称展开到 ±limit，limit 取数据绝对值最大值与
  柔性上下限的较大者。只对折线与柱状面板生效，其它面板类型按
  `axis display options are only available on time series and bar chart panels` 拒绝。
- 与固定 `min`/`max` 互斥（固定值会直接决定坐标轴，居中必然失效），按
  `centered zero needs an automatic axis without fixed limits` 拒绝；柔性上下限仍可与居中同时使用。
- 没有任何可用正数（全零样本或空数据）时保持原坐标轴，避免出现 `[0, 0]` 这种退化刻度。
- 前端：面板编辑器轴选项区新增「零值居中」开关，保存前按同一组规则拦截；切换可视化类型自动清理该字段；
  看板导入导出同步携带并校验（`centered_zero: false` 按缺省收敛）。
- 实测（8081）：接口 7 个用例符合预期（居中、柱状+柔性上限、`false` 201 且回读一致；
  固定上下限冲突、stat、table 都 400）。
  浏览器演练 `net_recv` 探针：线性刻度为 `0 … 585.94 KiB/s`（无负值）；打开零值居中后刻度变成
  `-412.07 / -167.93 / 76.21 / 412.07 KiB/s`（关于 0 对称）；再填固定 `min=0` 时提示
  `Centered zero needs an automatic axis without fixed limits.` 并阻止 Apply；关掉后回到原刻度，
  页面无错误，探针看板已清理。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/ ./internal/api/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（238 文件 1383 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,019 B + 最大语言分片 14,333 B = 255,352 B / 256,000 B）。

## v118：显示类轴选项（Grafana Axis → Show grid lines / Placement）（2026-10-17）

- 能力：`DashboardPanel` 新增 `grid`（`false` 关掉水平网格线，留空等于 `true`）与
  `axis_placement`（`left`/`right`，留空等于 Grafana 的 auto，即左侧）。两者都只对折线与柱状面板生效，
  其它面板类型按 `axis display options are only available on time series and bar chart panels` 拒绝，
  未知轴位置按 `axis placement must be left or right` 拒绝。
- 渲染：按开关决定是否画水平网格线，并把 Y 轴画在绘图区左侧或右侧。
- 前端：面板编辑器轴选项区新增「显示网格线」开关与「轴位置」下拉，保存前按同一组规则拦截；
  切换可视化类型自动清理这两个字段；看板导入导出携带并校验（`grid: true` 与 `left` 按缺省收敛）。
- 实测（8081）：接口 8 个用例符合预期（`grid false`/`grid true`/右侧/左侧/四项组合 201 且回读一致；
  `axis_placement: "auto"`、stat 上的网格线、table 上的轴位置都 400）。
  浏览器演练 `net_recv` 探针：缺省 5 条水平网格线且 Y 轴刻度在绘图区左侧（x=347 < 中心 532）；
  关掉网格线并改成右侧后网格线为 0 条、Y 轴刻度移到 x=655（右侧）；恢复默认后回到 5 条网格线与左侧轴，
  页面无错误，探针看板已清理。
- 验证中修正：英文文案原为「Placement」，与实际同时存在的图例「Placement」同名，改为「Axis placement」，
  避免面板编辑器里出现两个同名控件。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/ ./internal/api/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（238 文件 1380 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,020 B + 最大语言分片 14,333 B = 255,353 B / 256,000 B）。

## v117：Y 轴标题（Grafana Axis → Label）（2026-10-17）

- 能力：`DashboardPanel` 新增 `axis_label` —— 坐标轴旁的一行短标题，最多 40 字节、合法 UTF-8、
  不含控制字符（换行与制表符同样拒绝）；只对折线与柱状面板生效，其它面板类型按
  `axis label is only available on time series and bar chart panels` 拒绝。空值等于不显示标题。
- 渲染：标题旋转 -90° 贴在 Y 轴内侧，并在有标题时为 Y 轴多留 18px，避免压住刻度文本。
- 前端：面板编辑器新增「坐标轴标题」输入（`maxLength=40`）与说明，保存前按同一组规则拦截
  （按 UTF-8 字节数，而不是字符数）；切换可视化类型自动清理该字段；看板导入导出同步携带并校验。
- 实测（8081）：接口 10 个用例符合预期 —— 40 字节 ASCII、11 个汉字（33 字节）、空值都 201；
  41 字节 ASCII、14 个汉字（42 字节）、含换行的标题 400；stat/table 上的标题 400。
  浏览器演练：`net_recv` 探针设置「接收速率 KiB/s」后 SVG 出现 `rotate(-90, 5, 99)` 的轴标题，
  改成 14 个汉字触发 `Axis label must be 40 bytes or less without control characters.` 并阻止 Apply，
  清空后标题消失，页面无错误，探针看板已清理。
- 验证中发现并修复的真实缺陷：服务端原本只拦 NUL，会接受含换行的标题，而前端按「无控制字符」拦截，
  两者口径不一致（服务端接受、前端拒绝）。服务端改为与前端同口径后重新部署本版本。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/ ./internal/api/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（237 文件 1373 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,028 B + 最大语言分片 14,333 B = 255,361 B / 256,000 B）。

## v116：Y 轴刻度类型（Grafana Axis → Scale）（2026-10-17）

- 能力：`DashboardPanel` 新增 `scale`，留空与 `linear` 等效，`log` 表示以 10 为底的对数刻度。
  只对折线与柱状面板生效；未知值按 `axis scale must be linear or log` 拒绝，
  其它面板类型按 `axis scale is only available on time series and bar chart panels` 拒绝。
- 对数轴不接受非正下限：`min` 或 `soft_min` 小于等于 0 按 `logarithmic axis needs a minimum above zero`
  拒绝（0 与负数在对数轴上没有位置）。渲染器把 0 与负数样本当作缺失值（与 Grafana 一致），
  并把百分比单位缺省的 0..100 换成按正样本自动取值，避免对数轴上出现 0。
- 前端：面板编辑器新增 Scale 下拉（Linear / Logarithmic (base 10)）与说明，保存前按同一组规则拦截；
  切换可视化类型自动清理该字段；看板导入导出携带并校验（`linear` 与未声明一样按缺省收敛）。
- 实测（8081）：接口 10 个用例符合预期（timeseries/barchart 的 log 201 且回读 `scale: "log"`、
  `min: 0` 与 `soft_min: -1` 400、stat/table 400、未知值 400）。
  浏览器演练 `net_recv`（bytes/s）探针：线性刻度 `0 / 97.66 KiB/s / … / 390.63 KiB/s`，
  切到对数后刻度变成 `3.91 / 9.77 / 39.06 / 97.66 / 292.97 KiB/s`（10 的幂分布），
  0 值样本被丢弃（118 → 117 个点）；`min=0` 时编辑器提示 A logarithmic axis needs a minimum above zero
  并阻止 Apply；切到 Stat 后 Scale 下拉消失，切回时间序列显示 Linear；页面无错误，探针看板已清理。
- 文档：`metric-workspace-contract.md` 补充 `scale` 段落。
- 门禁：`goimports-reviser`、`go build ./...`、`go test ./internal/model/ ./internal/api/...`、
  `golangci-lint run ./internal/model/...` 0 issues、`tsc -b`、全量 vitest（235 文件 1366 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,004 B + 最大语言分片 14,333 B = 255,337 B / 256,000 B）。

## v115：折线/柱状面板的 Soft min / Soft max（2026-10-17）

- 能力（对齐 Grafana 的 Axis → Soft min / Soft max）：`DashboardPanel` 新增 `soft_min` / `soft_max`。
  柔性上下限只在数据超出时把坐标轴撑开，数据没超出时轴就从柔性值开始；固定 `min`/`max` 仍然优先，
  百分比单位的 0..100 缺省保持不变。
- 服务端校验：必须是有限数值；两侧同时给出时 `soft_min < soft_max`；只在折线与柱状面板上生效；
  同一侧已经有固定上下限时直接拒绝（这种组合下柔性值永远不会生效）。
- 面板编辑器新增两个数字输入与说明，保存前按同一组规则拦截并给出本地化提示；
  切换可视化类型时自动清理这两个字段，看板导入导出同步携带并校验。
- 实测（8081，percent 单位探针看板）：缺省刻度 0/25/50/75/100%；
  `soft_min=-20`、`soft_max=180` 后刻度变成 -20/30/80/130/180%；
  改成 5/90 后轴收进到 5..90%；把软下限与固定 `min` 同时填写时编辑器提示冲突并阻止 Apply。
  接口侧 11 个用例全部符合预期（含反转、等值、与固定上下限冲突、非折线面板拒绝），探针看板已清理。
- 文档：`metric-workspace-contract.md` 补充 soft axis 字段与规则。
- 门禁：`goimports-reviser`（internal/model）、`go build ./...`、`go test ./internal/model/ ./internal/api/...`、
  `golangci-lint run ./internal/model/...`、`tsc -b`、全量 vitest（234 文件 1359 例）、`vite build`
  通过公共外壳 gzip 预算（公共图 241,020 B + 最大语言分片 14,333 B = 255,353 B / 256,000 B）。

## v114：表格面板丢弃全空采样桶（2026-10-17）

- 缺陷修复（与 v113 柱状图同一根因）：查询按 `max_points` 返回定长桶网格，60 秒窗口也会给 600 个桶
  （实测只有 12 个桶有采样）。表格面板此前把每个空桶都渲染成一行「—」，
  100 行上限被空行占满——实测 100 行里 98 行是空行，真实采样反而看不见。
  - `workspaceTableRows` 新增 `dropEmptyBuckets`：表格面板开启，丢掉「所有序列都为空」的时间点；
    同一时间点上其它序列有值时，该行的空值仍然渲染（保留跨序列的缺口语义）。
  - 检查器（Inspect data）与 Explore 的结果表保持原始行为，继续显示全部桶，便于核对查询返回；
    实测同一看板的检查器仍是 111 行、其中 98 行空值行。
  - 面板的 CSV 导出与显示共用同一份行，因此导出也不再包含整行空桶。
- 实测（8081）：同一个 60 秒表格探针看板，修复前 `rows=100 / empty=98 / withValue=2`，
  修复后 `rows=11 / empty=0 / withValue=11`（CPU 采样值 99.09%、99.2%、48.42% …）；
  检查器保持 111 行（含空值行）；页面无错误，探针看板已清理。
- 变更文件：`webui/src/lib/workspace-table.ts`、`webui/src/components/metric-workspace/data-view.tsx`，
  并同步了 `workspace-table.test.ts` 与 `presentation.test.tsx` 的既有断言（行为变更本身即本版内容）。
- 门禁：`tsc -b`、全量 vitest（233 文件 1351 例）、`vite build` 通过公共外壳 gzip 预算
  （公共图 241,021 B + 最大语言分片 14,333 B = 255,354 B / 256,000 B）。

## v113：柱宽/柱圆角与柱状图亚像素柱宽缺陷修复（2026-10-17）

- 缺陷修复（真实缺陷，非能力新增）：查询按 `max_points` 返回定长桶网格——60 秒窗口也会给 600 个桶，
  其中 588 个是空桶。柱状图此前为每个空桶都留一个类目（600 个类目 ≈ 0.5px/个），
  柱子因此被压成约 0.5px 的亚像素细线（截图确认，几乎看不见）。
  渲染器现在先丢掉「所有序列都为空」的桶再画柱子：同一探针看板的柱宽从 0.5px 恢复到 12px。
- 能力：柱状图新增可选 `bar_width`（4..40px，缺省 12px，作为单根柱子的上限）与 `bar_radius`
  （0..10px，缺省 0），对应 Grafana Bar chart 的 Bar width 与 Bar radius。
  - 渲染：`bar_width` 取代写死的 `maxBarSize=12`；`bar_radius` 只圆化柱顶（柱脚仍贴住坐标轴）。
  - 校验：越界值按 `bar width must be between 4 and 40` / `bar radius must be between 0 and 10` 拒绝；
    其它面板类型带上它们按 `bar chart options are only available on bar chart panels` 拒绝。
  - 前端：面板编辑器新增「柱状图选项」区（只对柱状图显示），切换面板类型时清掉两个字段；
    可移植看板导入导出带上它们并做同样校验；五语文案补齐标签、提示与三条校验文案。
- 实测（8081）：接口 9 组用例通过——`bar_width` 4/40、`bar_radius` 0/10、只设 `bar_radius` 均 201 且回读一致；
  3/41、11、-1、折线带柱宽、条状仪表带柱圆角分别 400 并给出对应文案。
  浏览器演练：默认柱宽 12px、直角（`rounded=0`）；设柱宽 40 + 圆角 6 后柱子受类目宽度限制为 27px 且 10 根柱子全部带圆角；
  设柱宽 4 + 圆角 0 后柱宽 4px、无圆角；页面无错误，探针看板已清理。
- 门禁：`go build ./...`、`go test ./internal/model/ ./internal/api/...`、`golangci-lint run ./internal/model/...` 0 issues、
  `goimports-reviser`、`tsc -b`、全量 vitest（233 文件 1350 例）、`vite build` 通过公共外壳 gzip 预算
  （公共图 241,021 B + 最大语言分片 14,333 B = 255,354 B / 256,000 B）。

## v112：折线数据点大小（Grafana Point size）（2026-10-17）

- 能力：折线面板新增可选 `point_size`（1..12），对应 Grafana 的 Point size。
  - 未设置时沿用渲染器既有口径（`always` 2.6px、`auto` 1.8px），历史看板观感不变；设置后两种模式都用该半径。
  - 校验：越界或非整数按 `point size must be between 1 and 12` 拒绝；柱状图与其它面板类型带上它按既有文案拒绝；
    `show_points=never` 下带 `point_size` 按 `point size requires visible data points` 拒绝（此时点不画，配置不会生效）。
  - 前端：面板编辑器折线区新增「点大小」数字输入（`show_points=never` 时禁用并清空），
    渲染器按 `pointRadius` 取值；切换面板类型时清掉该字段；可移植看板导入导出带上 `point_size` 并做同样校验；
    五语文案补齐标签与两条校验文案。
- 实测（8081）：接口 8 组用例通过——`point_size` 6、1（auto）、12（叠加虚线）均 201 且回读一致；
  13、0、`never` 组合、柱状图、仪表盘分别 400 并给出对应文案。
  浏览器演练：`always` 且未设点时 SVG 数据点 `r=2.6`；设 6 后全部 `r=6`；切 `never` 后点大小输入禁用清空、
  数据点全部消失；切回 `auto` 后恢复 `r=1.8`；页面无错误，探针看板已清理。
- 门禁：`go build ./...`、`go test ./internal/model/ ./internal/api/...`、`golangci-lint run ./internal/model/...` 0 issues、
  `goimports-reviser`、`tsc -b`、全量 vitest（232 文件 1345 例）、`vite build` 通过公共外壳 gzip 预算
  （公共图 241,014 B + 最大语言分片 14,333 B = 255,347 B / 256,000 B）。

## v111：取值面板外观（Grafana Stat styles）（2026-10-17）

- 能力：`stat` 面板新增两个可选外观字段，对应 Grafana Stat 的 Color mode 与 Text mode。
  - `stat_color_mode`：`value` 缺省，把阈值色落在数值文字上；`background` 把阈值色铺成面板底色并按相对亮度
    选前景（`#f8fafc` / `#0f172a`）保证可读；`none` 完全不套色，迷你趋势图回退到当前文字色。
  - `stat_text_mode`：`auto` 缺省，数值下方照旧显示系列名与最近采样时间；`value` 只显示数值，标题栏仍保留面板标题。
  - 校验：未知取值按 `stat color mode is invalid` / `stat text mode is invalid` 拒绝；
    `stat` 之外的面板类型带上任一字段按 `stat styles are only available on stat panels` 拒绝。
  - 前端：面板编辑器「取值/Stat」区新增 Color mode 与 Text mode 两个下拉（非 stat 面板不显示），
    选回缺省值时省略字段；可移植看板导入导出带这两个字段并做同样校验；五语文案补齐。
- 实测（8081）：接口 7 组用例通过——`value`+`auto`、`background`+`value`、`none` 均 201 且回读一致；
  `stat_color_mode=gradient` 400（`stat color mode is invalid`）、`stat_text_mode=name` 400（`stat text mode is invalid`）；
  折线带 color mode 400、仪表盘带 text mode 400（均为 `stat styles are only available on stat panels`）。
  浏览器演练：`background` 时容器内联 `background-color: rgb(59, 130, 246); color: rgb(248, 250, 252)`，
  容器文本只有数值（不含系列名与采样时间）；切 `value` 后数值文字变 `rgb(59, 130, 246)` 且容器无底色；
  切 `none` 后数值与容器都无内联样式；切回 `auto` 后系列名（`CPU usage · 当前主机 · 实时采集`）与
  `Sample captured` 采样时间恢复显示；页面无错误，探针看板已清理。
- 门禁：`go test ./internal/model/`、`golangci-lint ./internal/model/...` 0 issues、`goimports-reviser`、
  `tsc -b`、定向 vitest（121 文件 747 例）、`npm run build` 通过分片预算。

## v110：图例位置（Grafana Legend placement）（2026-10-17）

- 能力：折线与柱状面板新增可选 `legend_placement`（`bottom` 缺省 / `right`）。
  - `right` 把外层布局改成横向 flex：绘图区被压窄、图例保持固定列宽并在右侧竖直排列；
    表格模式图例仍在固定宽度列里横向滚动。
  - 校验：只对 `timeseries` 与 `barchart` 生效，且要求 `legend` 为真；未知取值（含 Grafana 的 `top`）
    按 `legend placement is invalid` 拒绝，图例关闭或面板类型不符按
    `legend placement is only available on timeseries and bar chart panels` 拒绝。
  - 前端：面板编辑器图例区块新增「位置」下拉（折线/柱状可见，图例关闭时禁用），选回 Bottom 时省略字段；
    关闭图例时一并清掉该字段；可移植看板导入导出带上该字段并做同样校验。
- 实测（8081）：接口 8 组用例通过——折线 Right/Bottom、Right + 图例取值、柱状 Right 均 201 且回读一致；
  `top` 400、图例关闭带 Right 400、stat 面板 400、文本面板 400。
  浏览器演练：切到 Right 后图例列宽 160px、图表宽度收到 294px 自适应且容器 `items-stretch`，
  Bottom 布局不变；页面无错误，探针看板已清理。
- 门禁：`go test ./internal/model/`、`golangci-lint ./internal/model/...` 0 issues、`goimports-reviser`、
  `tsc -b`、定向 vitest（120 文件 742 例）、`npm run build` 通过分片预算。

## v109：连接空值（Grafana Connect null values）（2026-10-17）

- 能力：折线面板新增可选 `connect_nulls`（`none` 缺省 / `always`）。
  - `none` 保留采样缺口（Grafana 的 never），`always` 跨过缺口把折线连起来（Grafana 的 always）；
    只对 `timeseries` 面板生效，面积层与折线层共用同一个取值。
  - 校验：未知取值（含 Grafana 的 `threshold`）拒绝；柱状图与其它面板类型带上它拒绝，折线专属文案扩成
    「线宽、数据点标记、折线插值、折线线型与空样本处理只对折线面板生效」。
  - 面板编辑器折线区新增「空样本」下拉（柱状图不显示），选回「保留缺口」时省略字段；
    可移植看板导入导出带上该字段并做同样校验。
- 实测（8081）：接口 6 组用例通过——`always`（叠加填充）、`none`、
  `always`+虚线+平滑插值均 201 且回读一致；`threshold` 400（`null handling is invalid`）、
  柱状图 400（`line width, data points, line interpolation, line pattern and null handling are only available
  on timeseries panels`）、饼图 400（`line style is only available on timeseries panels`）。
  浏览器演练：编辑器下拉在 Keep gaps / Connect across gaps 之间切换并成功应用，面板持续渲染折线路径、
  页面无错误，探针看板已清理。**受限项（如实记录）**：当前 8081 的实时数据没有采样缺口，
  因此没能在真实数据上目视对比「连线 / 留缺口」两种画法，该行为只由样式单测与 recharts 契约覆盖。
- 门禁：`go test ./internal/model/ ./internal/api/...`、`golangci-lint ./internal/model/...` 0 issues、
  `goimports-reviser`、`tsc -b`、定向 vitest（metric-workspace / lib / context 119 文件 737 例）、
  `npm run build` 通过分片预算。

## v108：折线线型（Grafana Line style）（2026-10-17）

- 能力：折线面板新增可选 `line_pattern`（Grafana 的 Line style：Solid / Dashed / Dotted）。
  - 取值 `solid`（缺省）/`dashed`/`dotted`，只对 `timeseries` 面板生效；渲染成固定虚线数组
    （dashed `6 4`、dotted `1 3`），面积层与折线层共用同一个数组。
  - 校验：未知取值、柱状图带线型、其它面板类型带线型都会被拒绝；折线专属文案扩成
    「线宽、数据点标记、折线插值与折线线型只对折线面板生效」。
  - 面板编辑器折线区新增「折线线型」下拉（柱状图不显示），选回 Solid 时省略字段；
    可移植看板导入导出带上该字段并做同样校验。
- 实测（8081）：接口 6 组用例通过——`solid`（叠加填充与渐变）、`dashed`、
  `dotted`（叠加平滑插值）均 201 且回读一致；未知取值 400（`line pattern is invalid`）、
  柱状图 400（`line width, data points, line interpolation and line pattern are only available on timeseries
  panels`）、stat 面板 400（`line style is only available on timeseries panels`）。
  浏览器演练：`dashed` 时折线路径 `stroke-dasharray="6 4"`，`dotted` 时变 `1 3`，选回 `solid` 后属性消失；
  编辑器下拉选项为 Solid/Dashed/Dotted，页面无错误，探针看板已清理。
- 门禁：`go test ./internal/model/`、`golangci-lint ./internal/model/...` 0 issues、`goimports-reviser`、
  `tsc -b`、定向 vitest（metric-workspace / lib / context 119 文件 736 例）、`npm run build` 通过分片预算。

## v107：面板描述与信息图标（Grafana Panel description）（2026-10-17）

- 能力：面板 JSON 新增可选 `description`（Grafana 的 Panel description），任意面板类型（含 `text`/`row`）都可写。
  - 校验：≤512 字节、合法 UTF-8、不含 NUL；不参与查询与可视化校验，超限或非法按无效看板拒绝。
  - 前端：面板编辑器标题下方新增「面板描述」文本域与内联校验；面板头在标题旁渲染信息图标，
    `title` 与 `aria-label` 都是描述文本，后台工作区、公共节点页与壁板共用同一组件，因此行为一致。
  - 可移植看板导入导出带上 `description`，超长或含控制字符按整份文件无效处理；五语文案补齐三条。
- 实测（8081）：接口 8 组用例通过——ASCII/CJK 描述、正好 512 字节、`text` 面板与 `row` 面板的描述均 201
  且回读一致；513 字节（ASCII 与 CJK）、含 NUL 的描述 400（`panel description is invalid`）。
  浏览器演练：信息图标 `title`/`aria-label` 都是「当前主机的 CPU 使用率，超过 80% 需要人工确认」，
  编辑器回填同一文本；填入 171 个汉字后出现
  `Panel descriptions must be valid UTF-8 and at most 512 bytes.` 且「应用」被拦住（对话框不关闭）；
  改回合法描述后图标更新为「面板描述来自浏览器演练」，探针看板已清理，页面无错误。
- 门禁：`go test ./internal/model/`（新增 `TestValidateDashboardPanelDescription`）、
  `golangci-lint ./internal/model/...` 0 issues、`goimports-reviser`、`tsc -b`、
  定向 vitest（metric-workspace / lib / context 119 文件 735 例）、`npm run build` 通过分片预算。

## v106：迷你趋势图渐变填充（Grafana Sparkline gradient）（2026-10-17）

- 能力：stat 面板的迷你趋势图支持 Grafana 的填充渐变，面板 JSON 新增可选 `sparkline_gradient_mode`。
  - 取值 `none`（缺省）/`opacity`/`hue`：`opacity` 从序列色渐隐到透明，`hue` 保持亮度与透明度、
    旋转色相（`#3b82f6` → `#713bf6`）。与折线的 `gradient_mode` 同义，但只作用于迷你趋势图，
    因此单独成字段，折线的渐变仍只允许出现在 `timeseries`/`barchart` 上。
  - 校验：未知取值、非 `stat` 面板带渐变、未打开 `sparkline`、渐变模式下填充不透明度为 0 都会被拒绝；
    「需要先打开迷你趋势图」的文案扩成「线宽、填充不透明度与渐变模式」。
  - 面板编辑器迷你趋势图区块新增「渐变模式」下拉（折叠在开关之后），选渐变而填充为 0 时自动补 20%。
  - 迷你趋势图复用折线面板同一套渐变停靠点与 SVG id，可移植看板导入导出同步校验该字段。
- 实测（8081）：接口 10 组用例通过——`hue+30%`、`opacity+10%`、显式 `none`、`width 4+hue+60%`
  均 201 且回读一致；渐变无填充、渐变 0% 填充、未知取值、未开开关、`timeseries`/`gauge` 带渐变均 400
  （文案分别为 `sparkline gradient mode requires a non-zero fill opacity`、
  `sparkline gradient mode is invalid`、`sparkline line width, fill opacity and gradient mode require an
  enabled sparkline`、`sparkline is only available on stat panels`）。浏览器演练：Hue 面板的迷你趋势图出现
  `beat-gradient-v106-hue-A`（`#3b82f6/0.3` → `#713bf6/0.3`），Opacity 面板为
  `#16a34a/0.4` → `#16a34a/0`，面积路径引用该渐变；编辑器里「渐变模式」下拉只有一个（stat 面板），
  填充为空时选 Hue 自动补成 20 并渲染出 `#3b82f6/0.2` → `#713bf6/0.2`；探针看板已清理，页面无错误。
- 门禁：`go test ./internal/model/`、`golangci-lint ./internal/model/...` 0 issues、`goimports-reviser`、
  `tsc -b`、定向 vitest（workspace-sparkline / portable / line-style / 指标工作区 25 文件 / 190 例）、
  `npm run build` 通过分片预算。

## v105：折线插值（Grafana Line interpolation）（2026-10-17）

- 能力：折线面板支持 Grafana 的插值方式，面板 JSON 新增可选 `line_interpolation`。
  - 取值 `linear`（缺省）/`smooth`/`step_before`/`step_after`，只选择渲染器内置的四种曲线算法，
    不接受自定义路径，也不接受 Grafana 的驼峰写法（`stepBefore`）。
  - 柱状图与其它面板类型带上插值会被拒绝，错误文案改为「线宽、数据点标记与折线插值只对折线面板生效」。
  - 面板编辑器折线区新增「折线插值」下拉（柱状图不显示），选回 Linear 时省略字段。
- 实测（8081）：接口 9 组用例通过——`linear`/`smooth`/`step_before`/`step_after`、
  `smooth+20% 填充+hue 渐变` 均 201；`stepBefore` 驼峰、`cubic`、柱状图、stat 面板带插值均 400。
  浏览器演练从面板编辑器依次选择四种插值：路径从 `M52,175.44L...`（直线）变为
  `M52,175.367C52.21,...`（平滑）、`M52,176.087L52,175.197L52.63,...`（先保持后跳变）、
  `M52,176.807L52.63,176.807L...`（先跳变后保持）；刷新后未保存配置不落库，页面无错误。
- 门禁：`go test ./internal/model/ ./internal/api/...`、`golangci-lint ./internal/model/...` 0 issues、
  `goimports-reviser`、`tsc -b`、`oxlint`、全量 vitest 229 文件 / 1328 例。

## v104：渐变填充（Grafana Gradient mode）与折线填充消失缺陷修复（2026-10-17）

- 能力：折线面积与柱状填充支持 Grafana 的渐变模式，面板 JSON 新增可选 `gradient_mode`。
  - 取值 `none`（缺省）/`opacity`/`hue`：`opacity` 从序列色渐隐到透明，`hue` 保持亮度与透明度、
    旋转色相（`#3b82f6` → `#713bf6`），堆叠时分出层次。
  - 校验：未知取值、非 timeseries/barchart 面板带渐变、渐变模式下填充不透明度为 0 都会被拒绝，
    沿用以「保存不出渲染器会忽略的配置」为准的既有口径。
  - 面板编辑器新增「渐变模式」下拉；选渐变时若当前填充为 0 自动补 20%，因此不会出现「选了但不生效」。
- 缺陷修复：recharts 3 只在 `AreaChart`/`ComposedChart` 内渲染 `Area`，此前折线面板只要把
  `fill_opacity` 设为大于 0，整条序列就会消失（面积填充与堆叠从未真正画出来）。折线面板改用
  `ComposedChart`，面积、堆叠与渐变现在都实际渲染，回归用例锁住图表类型。
- 实测（8081）：接口 10 组用例通过——`hue+30%`、`opacity+10%`、显式 `none`、堆叠+渐变、柱状渐变无填充
  均 201；未知取值、渐变无填充、渐变+0% 填充、stat 面板带渐变、柱状 0% 填充均 400。浏览器演练：
  选择 Hue 后 CPU 面板出现 `beat-gradient-cpu-A`（`#3b82f6/0.2` → `#713bf6/0.2`），选 Opacity 后
  第二个停靠点变为 `#3b82f6/0`，面积路径引用 `url(#beat-gradient-cpu-A)`；仅设置填充不透明度时
  面板渲染 `recharts-area-area` + `recharts-area-curve`（修复前为 0 条路径）。刷新后未保存的演练
  配置不落库，页面无错误。
- 门禁：`go test ./internal/model/ ./internal/api/... ./internal/store/...`、`golangci-lint ./internal/model/...`
  0 issues、`goimports-reviser`、`tsc -b`、`oxlint`、全量 vitest 229 文件 / 1324 例。

## v103：主题制作指南补字段速查与拒绝原因自查（2026-10-17）

- 能力：`docs/beat/theme-author-guide.md` 从「逐字段叙述」补成「可查阅的手册」，且不需要新接口与迁移。
  - 新增第 9 节「字段速查表」：`beat-theme.json`、`theme.json`、`license.json` 与站点覆盖配置的
    全部可写字段集中成四张表，标注必需性与缺省值。
  - 新增第 10 节「常见拒绝原因自查清单」：把 `pack`/`validate`/市场发布/后台安装共用的生产校验器
    报错（`archive is invalid`、`manifest is invalid`、身份与摘要冲突、运行时版本不兼容、对比度不足、
    调色板令牌数量、多余文件、沙箱入口违规、预览空白）映射到原因与处理方式。
  - 第 2 节示例表扩成「适合的场景」「关键字段写法」两列，十套主题各自给出可直接抄的关键字段；
    第 8 节过时口径（`go test ./cmd/theme/` 在八套源码上重跑）改为十套。
- 实测（8081）：`/themes/theme-author-guide.md` 200/30 452 B（含「字段速查表」），十套 ZIP 摘要与第 8 节
  记录逐字节一致；`go test ./cmd/theme/ ./internal/theme/` 通过（分发资产逐字节复现）。
- 门禁：`bash scripts/build-themes.sh` 重建十套 ZIP/预览图/文档；`go test ./cmd/theme/ ./internal/theme/`。

## v102：新增两套内置主题 Midnight 与 Solar（2026-10-17）

- 能力：声明式主题从七套增至九套（合计十套含沙箱主题 Pulse），给用户更多可直接用的观感选择。
  - `themes/midnight`：宽幅大格 / 焦点详情 / 圆角 6 / 深蓝低光配色（`#0B1220` 深色底 + `#8AB4FF` 强调色）。
  - `themes/solar`：节点列表 / 均衡详情 / 紧凑行距 / 暖色高对比 + 琥珀强调（`#FFF8E7` 底 + `#8A4B00` 强调）。
  - 两套都带浅色与深色调色板、1440×900 预览图和 `license.json`，通过生产校验器对比度规则。
- 分发接线：`scripts/build-themes.sh` 循环、`cmd/theme/delivery_test.go` 交付清单、后台示例列表
  `webui/src/lib/theme-examples.ts` 同步；五语文案补 `theme.example_midnight`/`theme.example_solar`。
- 实测（8081，浏览器）：`/themes/midnight.theme.zip` 200/7 912 B、两套预览图 200 且为可解码 PNG；
  上传安装各返回 201（已安装 10 套）；十套主题逐一激活全部 200，渲染签名两两不同——Midnight
  `tiles/spotlight/comfortable`、半径 6px、`--background #F4F6FB`；Solar `list/balanced/compact`、
  半径 2px、`--background #FFF8E7`；演练结束后已恢复启用 Slate。
- 门禁：`go test ./internal/theme/ ./cmd/theme/` 通过（声明式九套互不相同、十套预览图 ≥320×180、
  分发资产逐字节复现）、`tsc -b`、`oxlint`、全量 vitest 227 文件 / 1310 例。

## v101：面板变换新增「按标签过滤」（2026-10-17）

- 能力：变换链新增 `filter_by_label`，按序列标签的 RE2 正则过滤，`invert` 反转命中集合；
  编辑器里叫「按标签过滤」，新建默认 pattern `^node-`，并带「反向匹配」开关，五语文案齐。
- 服务端：`DashboardTransformation` 新增可选 `invert`；缺 pattern、非法正则、超 128 字节 pattern、
  混用其它种类字段都拒绝；旧四种变换带上 `invert` 也一并拒绝（`rename accepts only pattern and replacement`）。
- 实测（8081，接口）：合法链（含 invert）返回 201；混用字段 / 缺 pattern / 坏正则 / rename+invert 分别 400
  并给出对应文案；探针看板已按 204 删除。浏览器：面板编辑器下拉含 Filter by label，选中后出现 Invert match。
- 门禁：`go test ./internal/model/... ./internal/api/...`、`goimports-reviser`、`golangci-lint` 0 issues、
  `tsc -b`、`oxlint`、全量 vitest 227 文件 / 1310 例。

## v100：变量选择同步地址栏（2026-10-17）

- 能力：看板变量选择写进 URL（`var-<name>=a,b`），打开带参链接即恢复同一组筛选；分享链接在
  `dashboard`/`node_id` 之外带上全部变量选择。
- 边界：单选变量只认第一个取值（手改 URL 不能绕过多选开关），空值与未声明变量忽略，写入时保留控制参数。
- 实测（8081，浏览器）：`node_id` 进入工作区后同步成 `var-node=…`；`var-node=ghost` 被丢弃并回落真实节点；
  分享链接含 `var-node=`。
- 门禁：`tsc -b`、`oxlint`、全量 vitest 227 文件 / 1306 例（新增 `workspace-url-state.test.ts` 6 例）。

## v99：自定义时间范围的 Grafana 风格表达式（2026-10-17）

- 能力：自定义时间对话框新增两个表达式输入框，接受 `now`、`now-30m`、`now-6h`、`now-2d`、`now-1w`，
  以及 `now/d`、`now/h`、`now/m`、`now/s`、`now/w`（UTC 对齐）与绝对时间 `2026-10-17`、
  `2026-10-17 09:00`、`2026-10-17T09:00:00Z`。
- 语义：表达式换算成 UTC 秒精度时刻后写回原有的日期时间输入，最终提交的仍是同一套绝对区间；
  非法输入（未知词、`now+1h`、`now-999999h`、`2026-02-31`、`24:00`）给出明确文案且不改动已选时间。
- 实测（8081，浏览器）：`now-6h` → 起点 `2026-10-16T19:36:18`；`now/d` → 终点 `2026-10-17T00:00`；
  应用后实际查询窗口 `from=2026-10-16T19:36:18`、`to=2026-10-17T00:00:00`；非法输入提示后改回合法值自动消失。
- 门禁：`tsc -b`、`oxlint`、全量 `vitest run` 226 文件 / 1300 例通过（新增表达式 5 例）。

## v98：粘贴面板 JSON（2026-10-17）

- 能力：把 v94 的只读检查器面板 JSON 补成复制/粘贴闭环。工具栏菜单新增「粘贴面板 JSON」，
  对话框粘贴从其它看板复制的面板定义，追加到看板末尾，保存前仍可编辑或删除。
- 校验：复用编辑器共享的 `validateWorkspacePanel`（面板类型、序列条数、stat/gauge 单序列、
  阈值、metric/聚合、链接与变换），解析与校验都在本地完成，取消或报错都不产生变更。
- 边界：空文本 / 非 JSON / 非面板对象 / 超 64 KiB / 看板已有 24 块分别给出明确文案；
  id 缺失、非法或与现有面板冲突时分配新 UUID，缺省 `series`/`thresholds` 按服务端默认值补空数组。
- 实测（8081，浏览器）：`{oops` 回「That JSON is not a panel definition.」；合法面板追加 1 块、
  再粘同一份得到 2 块（id 自动换新）、脏标记出现，刷新后看板未被改动（探针面板 0 块、revision 不变）。
- 门禁：`tsc -b`、`oxlint`、全量 `vitest run` 225 文件 / 1295 例通过。

## v97：主题声明摘要文案修正（2026-10-17）

- 资源行改用 `node.*` 文案域，CPU / 内存 / 磁盘不再显示原始 key；补四条内容开关文案
  （`theme.show_network`/`show_host`/`show_tags`/`show_remark`）五语齐全。
- 实测：Paper 展开后 `Resource rows: CPU · Memory`、`Hidden content: Network rates` 均为可读文案，
  展开前仍是 0 次预览请求，展开后 2 次。

## v96：主题包「声明摘要」（2026-10-17）

- 已安装主题卡片新增许可 / 最低版本 / 包体积元信息行，以及可展开的包声明摘要：运行时、协议号、
  沙箱入口、字体、资源行、被作者关闭的内容项，以及浅/深各 24 个调色板色块。
- 摘要按需调用既有只读接口，列表默认不发预览请求；实测 8 张卡片、展开 Paper 得到 48 个色块。

## v95：Explore 的「添加到看板」（2026-10-17）

- 能力：Grafana 的 Explore → Add to dashboard。探索页工具栏新增按钮，选中一块已有看板后把当前启用的
  查询行追加成时序面板，成功后跳到 `/admin/metrics?dashboard=<id>`。
- 切块与上限：`buildExplorePanels` 按 8 条序列一组切面板（服务端单面板上限），`appendExplorePanels` 分配未被占用的
  `explore-n` ID，并在超过 24 块面板上限时返回 null —— 调用方给出明确文案，不静默丢面板。
- 边界：面板继承目标看板的时间范围与变量；Explore 的临时时间范围不写进面板，避免看板被一次性探索钉死。
  Explore 的即时查询（instant）与隐藏行不参与转换，按钮在查询不可执行时禁用。
- 错误面：看板列表加载失败、没有看板、面板上限、保存失败各有独立文案，五语文案补 8 条。
- 实测（8081 浏览器）：默认查询行 → 空看板「Add panel probe」得到 `explore-1` 时序面板、1 条序列
  （`A`/`cpu`/默认节点），看板 revision 从 1 到 2，页面跳转到工作区，无错误边界；探针看板已删除。
- 门禁：`vitest run src/lib/explore-add-panel.test.ts`（3 例）、探索页与目录完整性用例、
  全量 223 文件 / 1285 例、`tsc -b`、`oxlint` 通过。

## v94：检查器「面板 JSON」标签页（2026-10-17）

- 能力：Grafana 的 Panel JSON。检查器在 Data / Stats / Query 之后新增只读的「面板 JSON」标签，
  显示当前面板定义（`JSON.stringify(panel, null, 2)`）并提供「复制 JSON」按钮；剪贴板不可用时沿用同页错误提示。
- 边界：这是面板定义，与已有的 Query 标签（查询请求 JSON）区分；不做就地编辑，避免绕过面板校验。
- 实测（8081 浏览器）：标签顺序 `Data / Stats / Query / Panel JSON`，JSON 含该面板的字段，
  复制后剪贴板内容与页面一致且按钮变为「JSON copied.」，无错误边界。

## v93：文本面板表格的跨行/跨列合并（2026-10-17）

- 能力：Markdown 表格新增两个整格标记——整格只有 `>>` 并入左侧格（colspan），只有 `^^` 并入上方格（rowspan）；
  两者可以叠用（先跨列、再被上一行跨行）。找不到合并目标时标记原样渲染成文本，表头不参与合并。
- 实现：`webui/src/lib/workspace-text.ts` 的表格行模型换成 `WorkspaceTextTableCell`（`column`/`spans`/`rowSpan`/`colSpan`），
  新增 `tableRows` 负责合并；`text-panel.tsx` 按跨度输出 `rowSpan`/`colSpan`，被合并的格子不再出现，
  HTML 表格自动占位；列对齐仍按锚定格所在列取，因此合并后不会丢对齐。
- 编辑器：文本面板提示新增一行表格说明（五语文案 `workspace.text_table_hint`）。
- 实测（8081 浏览器）：`node-a<br>node-b` 单元格得到 `rowspan=2`，顶格 `>>` 的 `disk` 得到 `colspan=2`，
  被合并的位置没有多余单元格，首格写 `>>`（左边没有格子）时按字面文本渲染，页面无错误边界。
- 门禁：`vitest run src/lib/workspace-text.test.ts`（19 例，新增合并正例与无目标回退）、
  指标工作区 21 文件 / 148 例、全量 222 文件 / 1280+ 例、`tsc -b`、`oxlint` 通过。

## v92：检查器 Stats 视图的缺口语义与表尾小计（2026-10-17）

- 能力：Stats 表从此能一眼看出覆盖情况——有缺口的序列用警示色标出，表尾给出整表小计。
- Gaps 列：`gaps > 0` 时用 `text-destructive` 高亮并带 tooltip（`workspace.legend_gaps_hint`），
  单元格加 `data-gaps` 属性便于批量核对；没有缺口时保持普通样式。
- 表尾合计：`workspaceStatsTotals` 汇总序列条数、有效采样、缺口与覆盖率（采样 / (采样 + 缺口)），
  没有任何分桶时覆盖率显示占位符而不是 0%。合计行只用于显示，CSV 仍逐条导出、不含合计。
- 实测（8081 浏览器）：默认看板的一个面板 Stats 表得到
  `Total · 1 series · 99% coverage`、采样 595、缺口 5，缺口单元格有高亮 span 与 tooltip，页面无错误边界。
- 门禁：`vitest run src/lib/workspace-stats.test.ts`（新增小计用例）、指标工作区 20 文件 / 111 例、
  全量 222 文件 / 1280 例、`tsc -b` 与 `oxlint` 通过。

## v91：表格面板的单元格显示模式（2026-10-17）

- 能力：表格面板新增 `cell_display_mode`（Grafana 的 Cell display mode）——Text only（缺省）、Color text、
  Color background、Gradient；阈值调色板从此也能用到表格的值列上。
- 服务端：`internal/model/dashboard_table.go` 定义 `DashboardCellDisplayModes` 枚举与 `validateDashboardTable`，
  只允许 table 面板携带该字段，枚举外取值与「非表格面板带字段」都按无效看板拒绝
  （`cell display mode is invalid` / `cell display mode is only available on table panels`）。
- 前端：`webui/src/lib/workspace-table.ts` 新增 `panelCellDisplayMode`、`cellDisplayModeError`、
  `tableCellStyle`。值映射颜色优先，其次按阈值取色：text/color_text 只改文本颜色，
  color_background 给单元格上色并用相对亮度挑可读前景，gradient 在当前阈值区间内插值背景透明度（15%–85%）。
  面板编辑器在表格类型下出现「单元格显示模式」下拉，选回 Text only 时省略字段；可移植看板导入同样校验该字段。
- 实测（浏览器）：`color_background` 单元格拿到 `background-color: rgb(22, 163, 74)` + 浅色前景；
  `gradient` 在 27% 采样上得到 `rgba(22, 163, 74, 0.39)`；缺省模式没有内联样式（历史行为不变）；
  编辑器下拉四个选项；未知模式与非表格面板经 API 创建分别返回 400 与对应文案；页面无错误边界。
- 门禁：`go test ./internal/model/`（新增 `dashboard_table_test.go`）、`golangci-lint run ./internal/model/...`、
  前端 `tsc -b`、全量 `vitest run`（222 文件 / 1279 例，含 `workspace-table.test.ts` 新增的单元格模式用例）。

## v90：面板与看板链接的图标选择（2026-10-17）

- 能力：面板数据链接与看板导航链接都能选图标（External link / Dashboard / Alert / Documentation / Info / Link），
  与 Grafana 的链接编辑体验对齐；缺省等于 External link。
- 服务端：`internal/model` 新增 `DashboardLinkIcons` 固定枚举，面板链接与看板链接共用 `icon` 校验，
  未知取值按无效看板拒绝（400 `dashboard link title, URL or icon is invalid`）。
- 前端：新增 `WorkspaceLinkIcon`（渲染）与 `WorkspaceLinkIconSelect`（编辑器下拉），
  面板底部链接行、文本面板链接行、看板链接条都按图标渲染；面板链接编辑器与看板详情编辑器都可选图标，
  选回 External link 时省略字段；可移植看板导入会解析并校验 `icon`，非法取值按整份文件无效处理。
- 实测（浏览器）：看板链接 `docs`/缺省/`alert` 分别渲染 `lucide-book-open`/`lucide-external-link`/`lucide-triangle-alert`，
  文本面板链接 `dashboard`/缺省 渲染 `lucide-layout-dashboard`/`lucide-external-link`，两处编辑器下拉都有六个选项，
  未知图标经 API 创建返回 400，页面无错误边界。
- 验证：`go test ./internal/model/`（新增 icon 正/负例与条数上限用例）、`golangci-lint run ./internal/model/...`、
  前端 `tsc -b`、全量 `vitest run`（222 文件 / 1276 例）通过。

## v89：主题制作指南补 `license.json` 与最小可安装包（2026-10-17）

- 能力：`docs/beat/theme-author-guide.md` 新增「3.1 `license.json`：随包许可说明」与「最小可安装包」，
  写明 `license`/`copyright`/`text` 三个字段、与 `beat-theme.json` 的关系、服务端并不解析 `license.json`
  （只有 manifest 的 `license` 字段参与校验）、它同样进入 `files` 清单并参与整包摘要，以及第三方资产的许可归属要求。
- 同步：`bash scripts/build-themes.sh` 重新生成八套 ZIP/预览图并把指南写入
  `webui/public/themes/theme-author-guide.md`（逐字节一致），八套 ZIP 摘要与指南第 8 节记录保持一致。
- 实测：`/themes/theme-author-guide.md` 200/25389 字节，slate ZIP sha256 与文档一致。

## v88：/readyz 数据目录写探针（2026-10-17）

- 背景：故障注入演练发现数据目录写满时 SQLite 读取仍然成功，`/readyz` 继续报 ready，
  运维无法从就绪状态看出服务已经无法提交任何变更。
- 实现：`internal/store.SQLiteStore.Ready` 在连通性与 schema 版本之后加一次真实写探针
  （`os.CreateTemp` + 写入 + 删除，文件 0600，`:memory:` 跳过）；`sqliteCheck` 用
  `sqliteFailureDetail` 把 ENOSPC/EROFS/EACCES 归类成 `no space left on device`、
  `read-only file system`、`permission denied`，其它保持 `unavailable`，不回显内部路径。
- 实测（写满 24 MiB tmpfs 的临时实例）：`/readyz` 从 `ready` 变成
  `503 {"sqlite":{"status":"error","detail":"no space left on device"}}`，释放空间后立刻回到 ready。

## v87：存储故障不再伪装成 401（2026-10-17）

- 现象：数据目录写满后，`GET /api/v1/auth/session`、管理端列表与写入全部返回 `401 unauthorized`；
  `Authenticate` 把 `GetSessionByTokenHash`/`GetUserByID`/`TouchSession` 的存储错误一律吞成
  `ErrSessionInvalid`，客户端会被误导去重新登录（而登录同样写不进数据库），日志中也没有 ERROR。
- 修复：新增 `adminauth.ErrSessionStore` 与 `sessionStoreError`（保留底层原因），
  `cookiePrincipal` 只在 `ErrSessionInvalid` 时回 401，其余记录 `administrator session lookup failed`
  并回 503 `authentication unavailable`。
- 实测（同上演练）：写满后 session/list/write 全部 503 且日志出现
  `touch session: ... database or disk is full (13)`；释放空间后立即恢复 200/201。

## v86：文本面板表格单元格换行（2026-10-17）

- 能力：Markdown 表格支持 `<br>`、`<br/>`、`</br>`（表头与表体共用），渲染成真正的换行；
  其它 HTML 仍按普通文本转义，行内代码/粗体/链接在换行片段里照常解析。
- 实测（浏览器）：探针文本面板里 4 个单元格渲染出 3 个 `<br>`，正文没有出现字面量 `<br>`，探针看板已删除。

## v85：查询历史排序与「仅显示星标」（2026-10-17）

- 能力：查询历史对话框新增「排序方式」（最近/最早，稳定排序，时间不可解析的条目排在末尾）与
  「仅显示星标」开关；键盘导航、全选、批量删除都作用在过滤后的列表上；五种语言文案已补齐。
- 实测（浏览器）：`/admin/metrics/explore` 跑一次查询后打开历史，排序下拉只有 `Newest first` / `Oldest first`
  两项；打开「仅显示星标」后未加星标的唯一条目被过滤掉并显示空态，页面零错误边界。

## v84：Stat 迷你趋势图按真实时间定位（2026-10-17）

- 能力：`sparklineAxis` 用首末采样时间把点线性映射到 canvas 宽度，不匀采样不再被压成等距；
  时间缺失、非有限值或整段同一时刻退回等分索引，旧数据与静态快照仍能画出完整折线；
  填充区域复用同一坐标。
- 验证：`webui/src/lib/workspace-sparkline.test.ts` 新增 4 例（含不匀采样与回退），`tsc -b` 与 `oxlint` 干净。

## v83：备份导出预算修复（2026-10-17）

- 现象：8081 上 `POST /api/v1/admin/backups` 直接落成 `failed`，原因是 MTS 全量导出没设读取预算，
  引擎默认的 100 万样本保护让 `agent_ingest_receipt` 报
  `read budget exceeded: samples actual=1012044 limit=1000000`。
- 修复：归档行上限与导出预算统一为 `model.MaximumBackupMetricRows`（1e9）；`internal/store` 导出查询
  显式带上该预算，超出仍返回可识别的预算错误而不是静默截断。
- 实测：修复后同一次备份 15 秒完成，`metric_rows=4751965`、归档 69.8 MB；
  67 分块断点续传（客户端中途退出后续传）finalize 后 `validated` 且行数与源归档一致；
  `stage-restore` + 重启后会话清空、marker 分组回滚消失，`/readyz` 仍为 `ready`。
- 无数据库迁移，schema 仍 v28；回滚用 `./deploy-preview-8081.sh rollback <备份目录>`。

## v82：后台窄视口横向溢出修复（2026-10-17）

- 入口：390px 宽度下的 `/admin/metrics`、`/admin/enrollment`、`/admin/settings`、指标工作台工具栏。
- 现象：入口按钮排与页签条不换行也不滚动，把页面撑出横向滚动条；英文/日文/印尼语下溢出 18–88px。
- 修复：动作行改 `flex-wrap`；三处页签条补 `max-w-full overflow-x-auto scrollbar-none`（与 alerts/security 一致）。
- 实测：「24 后台路由 × 4 语言（en/zh-TW/ja/id）@390px」横向溢出从 6 处降到 **0**；公共首页/壁板/节点页
  在 390/768/1440 一直为 0；Service Worker 离线外壳与 `/manifest.webmanifest`、`/sw.js`、192/512 图标均 200。
- 纯前端改动，无数据库迁移，schema 仍 v28。

## v81：示例主题卡片预览图（2026-10-17）

- 入口：`/admin/settings` → 「已安装主题」下方的示例主题区。
- 能力：八套内置示例（Slate、Operations、Canvas、Graphite、Aurora、Terminal、Paper、Pulse）各显示随 ZIP 分发的
  `/themes/<id>.preview.png` 缩略图，与已安装主题卡片的展示方式一致，挑主题时不必先安装再看效果。
- 实现：`examplePreviewURL(id)` 只对已知 id 拼路径（未知 id 返回 `null`），卡片用 `loading="lazy"` 的 `img`。
- 实测：浏览器打开设置页后，页面里 16 张预览图（8 张已安装主题 + 8 张示例）全部按 1440×900 实际加载，
  零失败请求（登录前的 `401 /auth/session` 是预期行为）。
- 纯前端改动，无数据库迁移，schema 仍为 v28。

## v80：只读视图隐藏链接开关（2026-10-17）

- 入口：`/admin/metrics` → 「Dashboard details」→ 「只读视图隐藏链接」。
- 能力：打开后，公开节点页与壁板会去掉看板链接条与所有面板数据链接（含文本面板正文下的链接）；
  后台工作区照常显示，方便继续编辑；缺省或关闭保持既有行为。
- 契约：`DashboardDefinition` 新增可选布尔 `hide_links`；导入导出同步校验，非布尔值按整份文件无效。
- 实测：在公开节点上临时发布一个节点看板（同一份定义先不带、再带 `hide_links`）——
  不带时只读页渲染出 1 条看板链接条 + 1 处面板数据链接，`PUT ... {"hide_links":true}` 返回 200 且回读为 true 后，
  同一页面两处计数都变成 0；探针看板随后用 SQLite 定向删除（服务端禁止删除非个人看板），
  看板列表回到原有 4 个，公开节点页恢复渲染 `Node overview`、零失败请求。
- 无数据库迁移，schema 仍为 v28。

## v79：文本面板表格列对齐（2026-10-17）

- 入口：`/admin/metrics` → 文本面板正文里的 Markdown 表格。
- 能力：分隔行里的冒号决定该列的对齐——`:---` 左对齐、`:---:` 居中、`---:` 右对齐、`---` 沿用默认左对齐；
  表头与单元格同时生效，列内容仍走同一套行内渲染（粗体/斜体/行内代码/链接）。
- 实测：写入「名称 / 数值 / 备注」三列表格（左 / 右 / 居中）后，浏览器计算样式分别为
  `left`、`right`、`center`，表头与数据行一致，单元格文本 `cpu`/`42`/`ok` 正常，探针看板已清理。
- 纯前端改动，无数据库迁移，schema 仍为 v28。

## v78：Stat 迷你趋势图的线宽与填充（2026-10-17）

- 入口：`/admin/metrics` → 编辑值面板（Stat）→ 「迷你趋势图」打开后出现「线宽」「填充不透明度」。
- 能力：迷你趋势图新增两个样式项——线宽 1–4（缺省 2，用 `non-scaling-stroke` 保证面板缩放时线宽不变）、
  填充不透明度 0–100%（缺省 0，只画线）；填充为每段折线画出落到基线的闭合区域。
- 契约：`DashboardPanel` 新增可选 `sparkline_line_width`、`sparkline_fill_opacity`；两者都要求
  `sparkline: true`，关闭开关或出现在非 stat 面板上都会被服务端拒绝；导入导出同步校验，越界按整份文件无效。
- 实测：服务端矩阵 7/7 通过——`stat` + `width4/fill60` 与 `width1/fill0` 均 201 且回读一致；
  `width5`、`fill101`、缺开关、开关关闭带填充分别 400（`sparkline line width must be between 1 and 4`、
  `sparkline fill opacity must be between 0 and 100`、`sparkline line width and fill opacity require an enabled
  sparkline`），`timeseries` 带选项 400（`sparkline is only available on stat panels`）；
  浏览器里该面板渲染出 `stroke-width=4` 的折线与 1 条 `fill-opacity=0.6` 的填充路径，编辑器两个输入框回显 4 与 60，
  页面除预期的 400 外零失败请求；探针看板已清理（看板列表回到原有 4 个）。
- 无数据库迁移，schema 仍为 v28。

## v77：文本面板表格与全量变量占位符（2026-10-17）

- 入口：`/admin/metrics` → 文本面板正文；链接编辑器里的占位符提示。
- 能力：正文支持 Markdown 表格（`| 表头 |` + `| --- |`，单元格支持粗体/斜体/行内代码/链接，可横向滚动）；
  正文里的链接开始解析 `${from}`/`${to}`/`${var-x}`/`${__all_variables}`。
- 实测：渲染出「阈值 / 处置」表头与两行数据，单元格内 `strong`/`code` 生效，
  `${__all_variables}` 链接解析为 `/admin/metrics?var-node=e9e36fea-1687-…`，零失败请求。
- 纯前端改动，无数据库迁移，schema 仍为 v28。

## v76：Table 面板列排序与 CSV 导出（2026-10-17）

- 入口：`/admin/metrics` → 表格面板表头与面板顶部「Download table CSV」。
- 能力：表头可点排序（时间戳/序列/数值，`aria-sort` 同步、空值恒排最后、稳定排序），
  可把当前可见行导出为 CSV（ISO 时间戳、原始精度），行数超过 100 时保留既有提示。
- 实测：100 行渲染、点「Value」两次切到 `descending`（首行 99.33%）、点「Timestamp (UTC)」升序，
  下载 `Samples.csv`，页面零失败请求。
- 纯前端改动，无数据库迁移，schema 仍为 v28。

## v75：Stat 面板迷你趋势图（Sparkline，2026-10-17）

- 入口：`/admin/metrics` → 编辑 stat 面板 → 「Show sparkline」开关。
- 能力：值面板在数值下方画出该序列的近期走势，空值处断线，线色跟随阈值取值。
- 契约：`DashboardPanel` 新增可选 `sparkline`（布尔）；只允许 stat 面板，其余类型（含 text/row）带上它一律拒绝。
- 实测：服务端 stat + `sparkline` 201 且回读为 true；`timeseries` 带 `sparkline` 400
  （`sparkline is only available on stat panels`）、文本面板带 `sparkline` 400；
  浏览器里该面板渲染出 `[data-workspace-sparkline]` SVG（4 段折线、`stroke=#3b82f6`），
  编辑器开关默认勾选，页面除预期的 400 外零失败请求。
- 无数据库迁移，schema 仍为 v28。

## v74：文本面板（Text panel，2026-10-17）

- 入口：`/admin/metrics` → 工具栏「Add panel」→ 「Visualization」选「Text」。
- 能力：面板可以只放说明文字（值班手册、阈值口径、排障入口），不查询数据也不占查询队列。
  Markdown 子集包含标题、无序/有序列表、代码块、行内代码、粗体、斜体与链接，另有最多 5 条面板数据链接。
- 安全：正文以 React 元素渲染，HTML 被转义而不是执行；链接只接受绝对 `http(s)` 或站内绝对路径，
  `javascript:`、`data:` 与协议相对 `//host` 都按普通文本保留。
- 契约：`DashboardPanel.type` 新增 `text`，并新增可选 `text`（≤4096 字节、UTF-8、禁控制字符）；
  文本面板带任何查询/可视化字段一律拒绝，非文本面板带 `text` 也拒绝。
- 实测：服务端合法定义 201 且回读一致；带 `series`、控制字符、4097 字节分别 400，
  非文本面板带 `text` 400；浏览器里写 Markdown 后渲染出标题/粗体/斜体/行内代码/列表/代码块与链接条，
  `<script>alert('xss')</script>` 以文本显示、`[点我](javascript:alert(1))` 不生成锚点，页面零失败请求。
- 无数据库迁移，schema 仍为 v28。

## v73：看板级导航链接（Dashboard links，2026-10-17）

- 入口：`/admin/metrics` → 工具栏「Dashboard details」对话框底部「Dashboard links」。
- 能力：每个看板最多 8 条导航链接，渲染在看板工具栏下方的链接条里（只读的公共节点页同样渲染），
  打开时带上看板变量与当前时间范围。
- 占位符：`${from}`、`${to}`、`${__url_time_range}`、`${__dashboard}` 与 `${var-<变量名>}`。
- 契约：`DashboardDefinition` 新增可选 `links[]`，沿用面板链接的标题/URL 校验；导入导出同步支持。
- 实测：`javascript:alert(1)` 编辑器就地报错并保持对话框打开；服务端 `javascript:` 400、9 条链接 400、
  合法 201 且回读一致；浏览器里加 `/admin/metrics?from=${from}&to=${to}&node=${var-node}` 后链接条渲染为
  `/admin/metrics?from=2026-10-16T19:37:30.106Z&to=2026-10-16T20:37:30.106Z&node=e9e36fea-…`（`target=_blank`），
  页面零失败请求。
- 无数据库迁移，schema 仍为 v28。

## v72：查询历史键盘导航与批量清理（2026-10-17）

- 入口：`/admin/metrics/explore` → 工具栏「Query history」。
- 键盘：搜索框里用 ↑/↓ 在筛选结果里循环高亮（Home/End 跳到首尾，从未导航状态按 ↓ 从第一条开始），
  Enter 直接打开高亮的那条；对话框里给出可发现性提示。
- 批量清理：每行新增勾选框，「全选」勾上当前筛选结果，「删除所选（n）」只删除勾中的条目并回报
  `Deleted n queries.`；原来的两步「清空历史」（保留星标）行为不变。
- 实测：3 条历史里按 ↓↓ 高亮第 2 条、End 跳到末条、再按 ↓ 回到第 1 条（循环）；勾 1 条后按钮显示
  `Delete selected (1)`，点击后状态栏显示 `Deleted 1 queries.`、列表剩 2 条且 `localStorage` 同步缩短；
  点「全选」后按钮变 `Delete selected (2)`；页面零失败请求。
- 无接口与数据库变更；新增 5 个管理端文案键（五语言同步），公共外壳预算不受影响。

## v71：检查器 Stats 排序、导出与 Gaps 列（2026-10-17）

- 入口：`/admin/metrics` → 任一面板「Inspect data」→「Stats」。
- 排序：点列头在升序/降序间切换，`aria-sort` 同步播报；数值列的空值固定排在最后，
  升序与降序都一样，值相等时保持原始序列顺序。
- 导出：新增「Download stats CSV」按钮（与页脚的整体 CSV 导出区分），表头用当前语言，
  数值保持原始精度，空值导出为空单元格，带 `﻿` BOM 便于 Excel 打开。
- 新增 `Gaps` 列：把缺样本的分桶数单列出来（此前只参与计算、界面上看不到）。
- 实测：`CPU usage` 面板显示 `576` 样本 / `24` 空桶；点 `Max` 后 `aria-sort=ascending`，
  再点变 `descending`；下载得到的 CSV 为
  `"Series","Min","Max","Mean","First","Last","Count","Gaps"` + `"0","99.61394769315605",…,"576","24"`，页面零失败请求。
- 无接口与数据库变更；新增公共文案键 `workspace.legend.gaps`、`workspace.export_stats_csv`（五语言同步）。

## v70：面板数据链接（Data links，2026-10-17）

- 入口：`/admin/metrics` → 任一面板右上角「⋯」→「编辑面板」→ 编辑面板对话框底部「数据链接」。
- 能力：每个面板最多 5 条链接，逐条填标题与 URL，可勾选「在新标签页打开」；链接以居中按钮条渲染在
  面板图表下方（Grafana 的面板链接位置），只读的公共节点页同样渲染。
- 占位符在打开链接时按当前面板上下文替换：`${node}`、`${node_name}`、`${group}`、`${group_name}`、
  `${metric}`、`${aggregation}`、`${from}`、`${to}`、`${__url_time_range}`，以及变量 `${var-<变量名>}`。
- 安全边界：服务端只接受绝对 `http(s)` 与站内相对路径，`javascript:`/`data:`/`//host` 一律 400；
  标题 1-40 字符、URL ≤1 KiB、行面板不接受链接；编辑器同样在保存前拦住非法协议。
- 面板新增可选字段 `links[]`，看板导入导出同步支持；无数据库迁移，schema 仍为 v28。
- 实测：`javascript:alert(1)` → 编辑器就地报错且「应用」不放行；服务端错误矩阵
  `javascript:` 400 / 6 条链接 400 / 行面板带链接 400 / `//evil.test` 400 / 合法 201 且回读一致；
  浏览器里把 `CPU usage` 面板加一条 `/admin/metrics?node_id=${node}&metric=${metric}&from=${from}`，
  应用后渲染为 `/admin/metrics?node_id=e9e36fea-…&metric=cpu&from=2026-10-16T19:17:28.847Z`（`target=_blank`），页面零失败请求。

## v66：自定义指标试运行（2026-10-17）

- 入口：`/admin/custom-metrics` → 任一指标行的「试运行」按钮。
- 流程：选一个已绑定节点 → 「运行试运行」→ 服务端排队 `custom_metric_probe` 任务 → Agent 执行一次命令 →
  对话框显示 `执行成功 / 执行失败`、失败原因与「命令输出」原始前缀。
- 实测：`uptime_seconds` 返回 `done · 4125343.31`；`load_avg` 返回 `failed · ... single number: "0.28,"`，
  输出 `0.28,`——该命令在本机多一个逗号，试运行直接把根因摆出来。
- 语义边界：试运行不改动节点本地采集注册表，也不写入指标历史；节点离线时任务保持 pending，重连后重放。
- schema 27 → 28（`agent_tasks` 增加 `output`/`failure`），冷备份 `pre-v66-20261016T194156Z`。

## v67：指标探索查询历史（2026-10-17）

- 入口：`/admin/metrics/explore` → 工具栏「Query history」。
- 能力：本地记录跑过的临时查询（最多 50 条，同查询只留最新并置顶）、可搜索、可加星标、
  星标条目在「清空历史」后保留，点「打开」按该条 URL 状态重新执行。
- 只记录用户主动执行与从历史恢复；自动刷新与回到前台补齐不写历史。
- 数据只存在浏览器 `localStorage`（`beat.explore.history.v1`），不涉及服务端 schema 与权限面。
- 实测：Run query → 打开历史显示 `Range · cpu · avg`，加星后本地存储为 `"starred":true`，页面零失败请求。

## v68：检查器 Stats 视图（2026-10-17）

- 入口：`/admin/metrics` → 任一面板右上角「Inspect data」→ 标签页「Stats」。
- 内容：每条序列一行，列出 Min / Max / Mean / First / Last / Count，数值沿用面板单位与语言格式；
  空桶不计入统计。实测 CPU usage 面板显示 `0% / 99.68% / 20.13% / 5.63% / 5.78% / 600`。
- 只读展示，不改动查询、URL 或保存状态；公共节点页的只读工作台同样可用。

## v69：回滚恢复修复 + 重启/回滚演练（2026-10-17）

- 修复：预览 `rollback` 不再用 `tar -x` 覆盖现有状态目录，改为「解到临时目录 → 整棵换入」，
  旧目录在服务就绪后才回收。旧行为会留下备份之后新建的 MTS 分片，让引擎长期报
  `mts: unavailable`，回滚后 `/readyz` 一直 503。
- 演练 1（systemd 重启）：停/启两个单元后采样无缝续上（20:01:10 → 20:01:32），readyz 23 项全 ok，
  journal 无 warning 以上条目。
- 演练 2（回滚）：用同一冷备份回滚，修复后脚本一次成功且 `/readyz` 立即 ready；随后已升回最新版。

本轮体验入口：

- GPU 与加速器：`/admin/accelerators`，选择节点管理采集策略；owner 可设置新节点默认策略。
- 指标工作台：`/admin/metrics`；面板编辑器类型下拉含「行」（分组折叠）与「Geo map」（地图面板）；节点接入：`/admin/agent-setup`。
- GPU 采集默认关闭。目标 Linux 节点需已有 `/usr/bin/nvidia-smi` 或 `/opt/rocm/bin/rocm-smi`；启用策略不会安装驱动和工具。
- 当前预览主机为 AMD Phoenix 核显，缺少上述采集工具，因此当前无 GPU 样本。真实 NVIDIA/AMD 驱动和硬件仍待用户实机验证，不能用自动化夹具结果替代。

## 2026-10-17 管理端目录按语言拆分（v59 部署）

管理端 22 份文案目录此前把 5 种语言全部内联，`admin-catalogs` 分块因此达到
**487.91 kB / gzip 131.87 kB**，再加一门语言只能继续撑大这个常驻分块。本轮按
`localization-parity-design.md` 已批准的「管理端目录按语言懒加载」完成拆分：

- 22 份 `context/<feature>-translations.ts` 只保留英文兜底目录；简体中文抽成
  `context/zh-cn/<feature>.ts`，与既有的 `context/zh-tw|ja|id/<feature>.ts` 一起由
  `context/admin-locale-shards/<语言>.ts` 合并，`context/admin-locales.ts` 按需加载后走既有的
  `registerLocaleCatalogs` 注册通道；
- `admin-catalogs` 分块降到 **93.47 kB / gzip 26.50 kB**；语言分片 `ja` 156.6 kB、
  `id` 133.0 kB、`zh-cn` 124.0 kB、`zh-tw` 122.5 kB，只下载当前语言；
- 公共外壳只在 `LocaleProvider` 里增加「目录迟到时重渲染」的订阅。同机对照构建（在 `/tmp`
  还原改动前的文件）测得公共图 240,269 B → 240,314 B（+45 B gzip），
  最大公共语言分片 13,711 B，合计 254,025 B / 256,000 B 通过；
- 管理端首屏不出现英文回退：入口在模块求值时发起语言分片请求，`AdminLocaleGate`
  在分片就绪前只渲染占位；英文（内联目录）不加等待，第一帧同步渲染。

浏览器实测（Playwright，8081，v59）：

- `zh-CN` / `ja` / `zh-TW` 三种语言直接打开 `/admin/security`，「账户安全」/「アカウントのセキュリティ」/
  「帳號安全」全部按预期出现，且每个语言只请求自己的两个分片（公共语言分片 + 后台语言分片），
  没有请求其它语言；
- 在后台把语言从 `zh-CN` 切换到 `ja`，安全页整体切换为日文，新增请求只有 `ja` 分片；
- `ja` 下扫描后台路由：`/admin/settings`（サイト設定）、`/admin/lifecycle`（所在地とライフサイクル）、
  `/admin/security`、`/admin/agent-releases`（エージェントのリリース）、`/admin/accelerators`
  （GPU とアクセラレーター）、`/admin/network`、`/admin/operations`、`/admin/metrics`
  （メトリクスワークスペース）全部命中日文目录，未出现英文回退文案；
- 公共首页在 `locale=ja` 下只下载 47 kB 的公共 `ja` 分片，不请求 156.6 kB 的后台 `ja` 分片。

门禁：`tsc -b` 0 错误、`vitest run` 213 文件 / 1195 例通过、`oxlint src scripts` 0 error、
`vitest run --coverage` 行覆盖 90.74%；新增 `context/admin-locales.test.ts` 与
`pages/admin-shell.test.tsx` 的「分片就绪前不渲染内容」用例，`catalog-integrity` 门禁保证
5 种语言键集完整。

部署：`0.2.0-dev+worktree.20261017.5`，发布目录
`/opt/beat-preview/releases/worktree-v59-20261016T182540Z`，冷备份
`/var/backups/beat-preview/pre-v59-20261016T182540Z`；schema 仍 v27、integrity ok、外键 0 违规。

## 2026-10-17 自定义指标采集健康与来源证据（v58 部署）

实机演练 C04/C05 时抓到一处静默失败：预览实例的自定义指标 `load_avg`
（`uptime | awk '{print $10}'`）从未产生任何采样，Agent 每 60 秒报
`custom metric value must be a finite number`。实测该命令在本机输出 `0.66,`（带尾逗号）——
采集器行为正确，但管理端既看不到失败原因，也看不到「从未收到采样」，只能看到一张空曲线。

### 后端：把已有的来源读取能力接出来

`MTSStore.QueryCustomSample`（最近一次 `custom_<name>` 采样 + 来源回执）此前只有存储层实现与
存储层用例，没有任何出口。新增 `GET /api/v1/custom-metrics/{id}/health`（owner/admin）逐绑定节点返回
`state`、`value`、`captured_at`、`report_at`、`age_seconds`、`sample_id`、`cadence_ms`、
`definition_version`：

- `ok`：最近采样在「三倍采集间隔」与「一分钟」的较大者之内；
- `stale`：超过该窗口，但证据仍然保留；
- `missing`：绑定后从未收到采样（本机 `load_avg` 的实况）；
- `unverified`：MTS 有数值但缺来源回执，按 C05「不允许纯 MTS 数值」的口径单列，不与正常采样混同；
- `disabled`：指标本身已停用。

未注入 MTS 存储返回 503、未知指标 404、读取失败 500。实机（8081，v58）：
`load_avg` → `{"state":"missing"}`；新建并绑定 `uptime_seconds`
（`awk '{print $1}' /proc/uptime`，15 秒）→ `{"state":"ok","value":4118308.79,
"sample_id":"91201e2f-…","cadence_ms":15000,"definition_version":"8f0f4cf6…"}`。

### Agent：采集失败原因不再是误导性的通用文案

`parseMetricValue` 区分「没有输出」「不是数字」「不是有限值」三种失败，并对非法输出回显
32 字符以内的前缀。部署后 Agent 日志由 `custom metric value must be a finite number` 变为
`custom metric command must print a single number: "9.48,"`，直接指出是命令格式问题。

### 前端：自定义指标「采集健康」面板

「查看数据」对话框新增健康区（`webui/src/components/custom-metric-health.tsx`）：按节点列出状态徽章、
数值、最近采样年龄、采集周期、定义版本与采样 ID 前缀；`missing`/`unverified`/`disabled`
分别给出可执行提示；健康接口不可用时仍渲染历史曲线。新增 12 个键 × 5 语言。

浏览器实测（Playwright，zh-CN，1500×1000，零失败请求）：
`uptime_seconds` →「采集健康 · 当前主机 · 实时采集 · 采集正常 4118384.29 · 最近采样 11s · 15s · 8f0f4cf6… · 957f68ca」+ 曲线；
`load_avg` →「无数据 · 未收到采样，请确认命令只输出一个数值。」+「暂无采集数据。」。

### 门禁

- 后端全量：`go test -race -shuffle=on -timeout 2400s ./...` 62 包全绿（`internal/backup` 1021s、
  `internal/notificationqueue` 953s、`internal/applicationsecrets` 890s、`internal/store` 827s）；
  `go test -coverprofile ./...` 0 FAIL、总语句覆盖 **90.1%**（`custom_metric_health.go` 的
  `HandleHealth` 83.3%、`customMetricHealth`/`customMetricStaleAfter` 100%，
  `parseMetricValue`/`truncateMetricToken` 100%）；
  `goimports-reviser` 0 行、`golangci-lint run ./...` 0 issues、`go vet` 干净。
- 前端：`vitest run` 212 文件 / 1190 例通过；`tsc -b` 0 错误；`oxlint src` 0 error；
  `vitest run --coverage` 行覆盖 90.75%。
- 新增用例：`internal/api/handler/custom_metric_health_test.go`（ok/stale/missing/unverified/disabled/
  未绑定/404/503/500 九类）、`internal/agent/custom_metric_parse_test.go`（错误分类与回显上限）、
  `webui/src/components/custom-metric-health.test.tsx`（状态渲染与未绑定提示）、
  `webui/src/pages/admin/custom-metrics.test.tsx`（健康区渲染 + 健康接口失败降级）。

部署：`release/preview-v58-20261017`，发布目录
`/opt/beat-preview/releases/worktree-v58-20261016T174421Z`，冷备份
`/var/backups/beat-preview/pre-v58-20261016T174421Z`；schema 仍 v27，integrity ok、外键 0 违规。

## 2026-10-17 擦除 worker 关闭误报与覆盖率门禁（v57 部署）

CI 的覆盖率门禁 `go test -coverprofile ./...` 在满载下抓到一处真实失败：

```
--- FAIL: TestServiceRunProcessesQueuedJobAndStops
    observer reports = [<nil> sql: transaction has already been committed or rolled back], run = <nil>
```

根因：上下文取消时 `database/sql` 主动回滚事务，驱动随后把后续语句报成 `sql.ErrTxDone`；
擦除 worker 的 `Run` 在 `ctx.Err() != nil` 时按正常关闭返回 `nil`，却仍把这条存储错误交给监督器，
造成「观察者报失败、循环报成功」的不一致，重启窗口内也会留下误导性的失败记录。

修法：新增 `Service.reportFailure(ctx, err)`，上下文已取消时把该类错误归一成 `context.Canceled`
再上报，未取消时原样上报。`Run` 的领取与执行两个分支都改走它；新增一例用例同时覆盖
「取消时归一」与「未取消时原样上报」。

门禁（本机全量复跑）：`go test -race -shuffle=on ./...` 62 包全绿；`go test -coverprofile ./...`
61 包 ok、0 FAIL、总语句覆盖 90.1%；`goimports-reviser` 无输出、`golangci-lint` 0 issues、
`go vet`、`go mod tidy -diff`、`go mod verify`、`govulncheck`（0 可达漏洞）、双端 `-trimpath` 构建
全部通过。

已知同类未改项：`internal/notificationqueue` 的 worker 循环同样是「先 observe 再判断
`ctx.Err()`」，关闭期间可能上报存储错误；尚无失败用例暴露该路径，按「未获同意不改动既有流程」
留作后续批次。

部署：`0.2.0-dev+worktree.20261017.3`，入口 `assets/index-Bdlp2Fnd.js`，
发布目录 `/opt/beat-preview/releases/worktree-v57-20261016T171741Z`，
冷备份 `/var/backups/beat-preview/pre-v57-20261016T171741Z`；schema 仍 v27。

## 2026-10-17 指标擦除 428 死路与 v55 前端门禁回归（v56 部署）

用户要求「继续推进所有待实现任务，每个功能点都更新 8081」。本轮按顺序修掉两处真实缺陷。

### 第 5 处 428 死路：指标擦除面板

`/admin/settings` → 「Erase history」页签首屏调用的 `GET /api/v1/admin/metric-erasure/jobs?limit=20`
走 `recentAuthOwnerAdmin`，fresh 登录后返回 428；面板此前只把后端英文
`recent authentication is required` 显示在告警条里，没有任何重认证入口（与 v54/v55 修掉的四处同类）。
本轮把统一机制接进来：

- `useErasureState` 用 `useReauthRetry` 包住「列表读取 / 提交擦除 / 重试 / 取消」四类动作，428 时
  整段动作挂起，确认身份后原样重放；非 428 错误沿用原有本地化文案。
- `metric-erasure-panel.tsx` 渲染统一 `ReauthDialog`；五语言补 `erase.error_reauth`。
- 实机验证（v56）：fresh 登录打开该页签 → 弹出「Confirm your identity」，
  网络面板仍只有被拦下的 `428 GET /admin/metric-erasure/jobs`，点击确认后按原动作重放。

### v55 遗留的前端门禁回归

v55 只跑了相关用例，全量前端门禁实际是红的，本轮补齐：

- `security-backup-panel.test.tsx`、`metric-erasure-panel.test.tsx`、`data-storage-section.test.tsx`
  共 9 例因 `useReauthRetry` 调用 `useAuth()` 而崩溃（`useAuth must be used within AuthProvider`）；
  三个测试文件补 `@/context/auth` mock，与仓库其余 37 个测试同一写法。
- `use-reauth-retry.test.tsx` 的 `vi.fn(async () => { throw ... })` 推断为 `Promise<never>`，
  `tsc -b` 报 `TS2322`；改为显式标注 `vi.fn<() => Promise<string>>`。

### 门禁与实机证据

- 后端 `go test -race -shuffle=on -timeout 2400s ./...`：62 个包全绿（含 `internal/api` 827s、
  `internal/api/handler` 753s、`internal/backup` 1042s、`internal/store` 846s）。
- 前端 `vitest run` 211 文件 / 1185 例通过；`tsc -b` 0 错误；`oxlint` 0 error（16 warning 既有）。
- 主题演练（C09/C10）：8081 已装八套主题，逐一套用 `POST /admin/themes/{id}/activate` 全部 200，
  公共首页 `[data-public-theme]` 容器分别渲染 `tiles/table/cards`、圆角 0–8px、palette 取值两两不同，
  零失败请求；沙箱主题 Pulse 以 `sandbox="allow-scripts"` 的 iframe 渲染 `dist/index.html`
  并在匿名会话中输出自己的状态板（无 401/控制台错误）。

部署：`0.2.0-dev+worktree.20261017.2`，入口 `assets/index-Bdlp2Fnd.js`，
发布目录 `/opt/beat-preview/releases/worktree-v56-20261016T163829Z`，
冷备份 `/var/backups/beat-preview/pre-v56-20261016T163829Z`；schema 仍 v27。

## 2026-10-17 接入授权与三处 428 死路（v54 / v55 部署）

现象：`/admin/enrollment` 的「接入授权」页签每次登录后都显示一行后端英文 `recent authentication is
required` 和一个永远点不成功的 `Retry`。根因是授权读写走 `recentAuthOwnerAdmin`，而普通登录不会写
`ReauthenticatedUntil`，只有显式重认证才会；该面板此前没有任何重认证入口。实测确认同一类问题还有三处：

| 位置 | 触发动作 | 服务端实测 |
| --- | --- | --- |
| `/admin/enrollment` → 接入授权 | 打开页签即读取授权列表 | `GET /api/v1/admin/agent-enrollment/grants` 428 |
| `/admin/operations` → 运行时诊断 | 开始采集、下载、删除采集件 | `POST /api/v1/admin/runtime/captures` 428 |
| 告警渠道 | 渠道启用开关 | `PUT /api/v1/alerts/channels/{id}` 428（同族路由） |
| `/admin/security` → 备份与恢复 | 新建/续传上传会话 | `POST /api/v1/admin/backups/uploads` 428 |

v54（`0.2.0-dev+worktree.20261016.16`）先修接入授权：新增
`webui/src/pages/admin/enrollment-proof-dialog.tsx`，授权状态新增 `status`，428 时改为本地化提示 +
「确认身份」按钮，确认后重新读取授权；五语言补齐 `discovery.proof_*`。

v55（`0.2.0-dev+worktree.20261017.1`）把同类处理抽成可复用机制并补齐其余三处：

- `webui/src/hooks/use-reauth-retry.ts`：被 428 拦下的动作整体（含成功后的刷新、关弹窗、写文件）
  挂起，确认身份后原样重放；非 428 错误继续抛给调用方，各面板原有错误展示不变。
- `webui/src/components/reauth-dialog.tsx`：统一弹窗（密码 + 启用 TOTP 时的 6 位验证码），文案放基础
  目录 `reauth.*`，五语言齐备；`use-reauth-retry.test.tsx` 4 例覆盖直接成功、428 挂起后重放、
  非 428 上抛、确认失败保留挂起动作。
- 接入点：运行时诊断面板、通知渠道启用开关、备份上传对话框。

已实测（fresh 登录、无近期重认证）：备份上传点击「Upload and validate」得到
`428 POST /api/v1/admin/backups/uploads` 并弹出「Confirm your identity」，不再停在英文错误上；
服务端校验语义未改动，确认身份后按原动作重放。订阅为空的预览实例上无法实机点开关，通知渠道开关
与运行时诊断（远程操作默认关闭，入口当前不可达）由同一机制与单测覆盖。

## 2026-10-16 构建切换并入版本巡检（v53 部署）

v52 新增的独立升级巡检（`use-build-patrol.ts`、`build-identity.ts`、`build-reload.ts`）与既有
`webui/src/lib/version-watch.ts` 职责重复：两条 60 s 循环各读一次 `/index.html`，且旧实现只判可见性。
本轮合并回单一机制并删除重复实现（`App.tsx` 不再引入那三个模块）：

- `version-watch.ts` 巡检间隔由 5 分钟改为 `INTERVAL_MS = 60_000`；仍在安装、`pageshow`、
  `visibilitychange` 时立即比对，同一入口分片只整页加载一次（`sessionStorage` 键
  `beat:version-reloaded`），探测失败静默跳过。
- 新增「不打断输入」判定：标签页不可见、存在 `[role=dialog]`/`[role=alertdialog]`、或焦点停在
  `input`/`textarea`/`select`/`[contenteditable]` 上时本轮不切换，下一轮再判。
- `version-watch.test.ts` 新增 3 例（默认一分钟巡检、编辑中保持、弹窗打开时保持）。

部署：`release/preview-v53-20261016`（后端与 Agent `0.2.0-dev+worktree.20261016.15`，commit `14fae14`，
构建时间 `2026-10-16T13:23:50Z`）→ `/opt/beat-preview/releases/worktree-v53-20261016T132414Z`，入口
`assets/index-DpKdDZDU.js`、SW `f6e583140db786df`、`version-watch-By0a7VLb.js`；冷备份
`/var/backups/beat-preview/pre-v53-20261016T132414Z`；schema 仍 v27，不改接口签名。

升级后复核（同一现象第四次上报，本轮只做证据核对，未改服务端）：

- 服务端：本机 8081 与 `https://beat.xuthus.cc` 的 `admin/metrics/catalog`、`admin/metrics/dashboards`、
  `nodes/manage`、`groups`、`metrics/catalog`、`metrics/dashboards/default` 全部 200，预览实例无 5xx。
- 浏览器：匿名与登录两种会话下，「首页 → 点击节点卡 → 节点页」均正常渲染指标工作区；后台
  `/admin/metrics` 目录、看板、面板告警与查询全 200。
- 旧构建对照：把 v30 的入口文档挂到当前服务端仍能正常渲染；v14 的入口只能改地址栏、页面不切换
  （即用户先前报告的「点击不跳转」）。结论是老页面必须整页重载一次，之后才由 v53 的巡检接管。

配套收尾（只动测试与文档，不改服务端行为，8081 无需重新部署）：删掉未使用的测试辅助函数后
`golangci-lint run ./...` 为 0 issues；把 `internal/store/dashboard_collaboration_test.go` 的填充 token
改为按序号生成（原实现取 UUID 前 4 位十六进制，32 条数据约 0.8% 概率撞 `token` 的 UNIQUE 约束）；
`go test -race -shuffle=on ./...` 抓到并修掉 `internal/applicationgc` 测试自身的数据竞争；
补 `versions.test.tsx`（4 例）后前端 `test:coverage` 通过阈值：210 文件 / 1180 用例、行覆盖 90.97%。

## 2026-10-16 升级后旧标签页自动切换构建（v52 部署）

用户再次报告「在宿主机上执行升级后访问报错 `workspace.load_failed`」。本轮把「升级窗口」本身当缺陷处理，
不再要求用户手动刷新。

排查证据（服务端与产物健康，问题在客户端构建状态）：

- v51 实例 `/healthz` = `0.2.0-dev+worktree.20261016.13`、`/readyz` 200、`schema 27`；用户出口
  `134.185.119.110`（经 `https://beat.xuthus.cc`）当日请求全部 200/304，工作区四个接口
  （`admin/metrics/catalog`、`dashboards`、`nodes/manage`、`groups`）与公共节点页接口均 200。
- 无头浏览器逐页复核 v51：`/admin/metrics`、`/admin/nodes`、`/admin/alerts`、`/admin/network`、`/`、
  `/node/:id` 无原始键名、无失败请求；注入 catalog 404 / `data:null`、整段指标接口断连 48 s 后恢复，
  页面都不显示原始键名并自动回到实时渲染（28 个图表元素）。
- 服务端日志显示该浏览器在 17:00、17:35 仍取 `assets/index-CYklynL5.js`（v49），19:47 取
  `assets/index-YLQEk2lF.js`：升级脚本换了入口分片，已加载完成的页面不会自己发现，发布目录又刻意
  保留上一版哈希分片，旧页面因此可以长期停在旧代码上；v43 之前的构建会把 `workspace.load_failed`
  当键名原样渲染。

修复（`webui/src/hooks/use-build-patrol.ts`、`webui/src/lib/build-identity.ts`、
`webui/src/lib/build-reload.ts`，挂载于 `webui/src/App.tsx`）：

- 每 60 s 以 `no-store` 读取 `/index.html`，比对服务端入口分片与当前页面入口分片；
- 不一致且标签页可见、没有弹窗、用户没在输入控件里时自动重载到新构建；
- 同一构建只自动重载一次（`sessionStorage` 记录），服务端回滚或代理缓存异常不会形成刷新循环；
- 开发构建（入口不是 `/assets/index-*.js`）不参与巡检，非 PROD 构建直接跳过。

验证（v52 实机）：

- `npx vitest run` 210 文件 / 1179 用例全通过（新增 `use-build-patrol.test.tsx` 6 例：换构建重载、
  同构建不动、守卫不重复重载、输入中不打断、隐藏标签页与探测失败忽略、非生产不巡检）；`tsc -b` 退出 0；
  `oxlint src` 仅既有 warning；`npm run build` 通过公共外壳与分片预算门禁。
- 端到端：加载 `/`（入口 `index-gsV9EOcA.js`）后注视 75 s —— 0 次导航、1 次探测（不误重载）；随后让
  `/index.html` 返回另一个入口分片，75 s 内页面自动导航到新构建（`navigations=2`）。
- 冒烟：`http://127.0.0.1:8081` 与 `https://beat.xuthus.cc` 两个入口的公共首页、`/node/:id`（28 个图表
  元素）、`/admin/metrics` 均正常渲染，无 `workspace.*` 原始键、无失败请求（匿名 `GET /api/v1/auth/session`
  的 401 属预期）。

部署：`release/preview-v52-20261016`（后端与 Agent `0.2.0-dev+worktree.20261016.14`，commit
`14fae14+dirty`，入口 `assets/index-gsV9EOcA.js`，Service Worker `4c66ea156bfc0780`，194 项校验通过）→
`/opt/beat-preview/releases/worktree-v52-20261016T131441Z`，冷备份 `/var/backups/beat-preview/pre-v52-20261016T131441Z`；
部署自检 reporting `schema 27 / integrity ok / fk violations 0`，部署后 `journalctl` 无 4xx/5xx。本批不改
SQLite schema、不改备份格式、不改任何接口签名。

该实现已在 v53 合并回 `webui/src/lib/version-watch.ts` 并删除 `use-build-patrol.ts`、
`build-identity.ts`、`build-reload.ts`，见本文最上一节。

## 2026-10-16 工作区错误态驻留恢复（v51 部署）

用户升级 8081 后再次报「访问报错 `workspace.load_failed`」。结论：服务端与产物健康，缺陷在错误态的恢复
预算。

排查证据：

- 部署自检：v50 实例 `/healthz` = `0.2.0-dev+worktree.20261016.12`、`/readyz` 200、`schema 27` /
  integrity ok / 外键 0；`journalctl` 自部署起无 4xx/5xx、无历史分片 404。
- 全新浏览器（登录 `preview`）实测 `/`、`/node/:id`、`/wallboard`、`/admin/metrics` 全部正常渲染，
  首页点击节点进入也正常，零控制台错误、零失败请求。
- 服务端日志里的入口分片请求显示长期标签页仍在跑旧构建：`192.168.1.48` 取
  `assets/index-BIngKcKj.js`（v38–v40）、`119.8.242.12` 18:06 取 `assets/index-CYklynL5.js`（v49）。
  v43 之前的错误提示会原样渲染键名 `workspace.load_failed`，v49 之前没有版本巡检，因此这类页面升级
  后必须手动刷新一次。
- 代码层缺陷（`webui/src/lib/workspace-retry.ts`）：首屏退避 6 次（约 29 s）后落到错误态，再按 20 s
  慢速恢复 15 次（约 5 分钟）便停止重试；错误态又只由 `online`/`pageshow`/`visibilitychange` 触发，
  前台标签页不发这些事件，于是升级或重启窗口一过就永久停在错误态。

修复：慢速预算用尽后转入驻留恢复，每 60 s 重试一次并持续（`workspaceIdleRecoveryIntervalMs`）；确定性
失败（400/401/403/404 等）仍立即停止。定向用例（`workspace-retry`、两个工作区 Hook）覆盖新节奏，
`npx vitest run` 209 文件 / 1173 用例全通过，`tsc -b` 退出 0，`oxlint src` 仅既有 warning，
`npm run build` 通过分片预算。

部署：`release/preview-v51-20261016`（后端与 Agent `0.2.0-dev+worktree.20261016.13`，commit `14fae14`，
构建 `2026-10-16T11:53:25Z`，入口 `assets/index-BdmJX53v.js`，194 项 SHA256 校验通过）发布到
`/opt/beat-preview/releases/worktree-v51-20261016T115356Z`，冷备份
`/var/backups/beat-preview/pre-v51-20261016T115356Z`；本批不改 schema（仍 v27 `dashboard_versions`）。

实机验证：注入 7 分钟全接口失败（长于旧预算 5 分钟）——期间页面显示本地化错误态；恢复网络后 30 s 内
自动回到实时渲染（`error=false`、4 个图表元素），全程没有刷新页面。修复前同样的时序会永久停在错误态。

## 2026-10-16 指标工作区 Geo map 面板（Grafana Geomap）与 v49 部署

新增 `geomap` 面板类型（第 12 种），把 Grafana 的 Geomap 落成可离线渲染的地图面板。

- 数据模型：`internal/model/dashboard_geomap.go` 定义 `DashboardGeoLocation`（`match`/`label`/`latitude`/
  `longitude`）与地点表（1..64 条、匹配串唯一且忽略大小写、纬度 ±85.05112878、经度 ±180）、图层
  `markers`/`heatmap`、缩放 0..8、标记 2..64、中心点必须成对、`geo_tile_url` 必须是含 `{z}{x}{y}` 的
  http(s) 模板且 ≤512 字节；geo 字段挂在非 `geomap` 面板上会被拒绝，`geomap` 与折线/柱状/热力图/直方图
  同组拒绝值映射（`dashboard_value_mapping.go`）。
- 离线底图：`webui/src/lib/workspace-geo-land.ts` 内置 Natural Earth 110m 陆地的 128×128 扫描线栅格 +
  run-length 编码（9,988 字节、陆地占比 38.4%），生成器 `webui/scripts/generate-geo-land.py` 可重复执行；
  渲染器 `components/metric-workspace/geomap.tsx` 输出 SVG 底图 + 经纬网 + 可选 XYZ 瓦片 + 标记/热力两图层，
  暴露 `data-geomap`、`data-geomap-land`、`data-geomap-marker`、`data-geomap-heat`、`data-geomap-tile`、
  `data-geomap-graticule` 供测试断言。
- 编辑体验：面板类型下拉新增「Geo map」；面板选项提供地点表增删改、图层、缩放、标记大小、中心经纬度与
  瓦片模板，并在 geomap 上隐藏值映射；切到 geomap 预置一条空地点，未填坐标按校验拦截。
- 搬运与文案：导入导出白名单接 `geomap`（另加 13 类非法负载被拒）；五语言共 24 条 `workspace.geomap*`，
  公共外壳补 4 条。地图代码与底图走按需分片（`assets/geomap-*.js` 8.71 kB、`assets/workspace-geomap-*.js`
  4.70 kB），不占公共外壳预算。

验证：`npx vitest run` 209 文件 / 1173 用例全通过（行覆盖 91.06%），`tsc -b` 退出 0，`oxlint src` 仅既有
warning，`npm run build` 通过全部分片预算（公共图 gzip 239,948 B + 最大语言分片 13,426 B = 253,374 B /
256,000 B，最大私有分片 `admin-catalogs` 487.61 kB / 600 KiB）；后端 `gofmt -l` 无输出、`goimports-reviser`
退出 0、`golangci-lint run ./internal/model/...` 0 issues、`go build ./internal/... ./cmd/...` 与
`go test ./internal/model/` 通过（新增 55 个 geomap 子用例）。

实机走查（`http://127.0.0.1:8081` 无头浏览器）：登录后建含 geomap 的看板 201、两次 PUT 到 revision 3；
越界纬度、重复匹配、未知图层、坏瓦片模板、越界缩放、半中心、非 geomap 混用均 400；地图渲染
`markers=1`、`land=2`、`heat=1`、`maps=2`、`unmatched=0`，无 `workspace.*` 原始键、无失败请求；面板编辑器
回显地点表/纬度/瓦片/缩放/标记输入，类型下拉完整文本为
`Time series / Bar chart / Stat / Gauge / Bar gauge / Pie chart / Heatmap / Histogram / Geo map / State timeline / Table / Row`。
2026-10-16 17:00 复核：`/healthz` = `0.2.0-dev+worktree.20261016.11`，`http://192.168.1.48:8081` 与
`https://beat.xuthus.cc` 的 `/`、`/admin/metrics`、`/node/:id`、`/wallboard` 均 200，`admin/metrics/catalog`、
`dashboards`、`nodes/manage`、`groups` 与指标查询均 200。

**`workspace.load_failed` 复核（2026-10-16 17:35）**：再次收到「宿主机升级后访问报
`workspace.load_failed`」。本轮以全新浏览器复核 `http://192.168.1.48:8081` 与 `https://beat.xuthus.cc`
的未登录与已登录（`preview`）身份：`/`、`/node/:id`、`/wallboard`、`/admin`、`/admin/nodes`、
`/admin/metrics` 全部正常渲染；`/healthz` = `0.2.0-dev+worktree.20261016.11`、入口
`assets/index-CYklynL5.js`、`/readyz` 200、schema v26 / integrity ok / 外键 0 违规；控制台除 Cloudflare
beacon 的既有 CSP 报错外零错误、零失败请求，日志也无 4xx/5xx 与旧分片 404。

本轮把根因落到产物层：预览部署按设计保留历史哈希分片（`deploy-preview-8081.sh` 的
`cp -an previous/static/assets`），v43 入口 `index-BCQBqBgE.js` 引用的
`workspace-retry-CoVq6ywZ.js` 不含 20 s 慢恢复节奏，而 v45 `index-D9wBIO40.js` →
`workspace-retry-BjdUXT6Y.js`、v49 `index-CYklynL5.js` → `workspace-retry-Cwg8C5tY.js` 均含。
因此升级前就已打开、且在服务端重启窗口内请求失败的 v43/v44 文档会永久停在错误态（旧代码不自愈），
而服务端和全新页面都健康。处置顺序：硬刷新 → PWA 全部窗口关闭重开 → DevTools 注销 Service Worker
并清站点数据；v49 起 `version-watch` 与 20 s 慢恢复会让后续升级自动整页重载，无需再手动刷新。

部署：`release/preview-v49-20261016`（后端与 Agent `0.2.0-dev+worktree.20261016.11`，commit `14fae14`，
构建时间 `2026-10-16T08:49:34Z`）发布到 `/opt/beat-preview/releases/worktree-v49-20261016T085038Z`，冷备份
`/var/backups/beat-preview/pre-v49-20261016T085038Z`；包内 194 项 SHA256 校验通过，schema 仍为 v26
`agent_discovery`、integrity ok、外键 0 违规，无接口签名与备份格式变更。

**预览实例 unit 依赖修正（2026-10-16 19:25）**：`beat-preview-agent.service` 原先写 `Requires=beat-preview-server.service`，
于是 `systemctl restart beat-preview-server.service` 会连带停掉 agent，而只 `start` 服务端不会把 agent 带回来，
节点随即离线（实测：19:11:16 服务端停止时 agent 被一起 Deactivated，节点 last_seen 停在 19:11:13）。
已改为 `Wants=beat-preview-server.service`（原文件备份为 `/etc/systemd/system/beat-preview-agent.service.bak-*`），
`systemctl daemon-reload` 后 `systemctl show -p Requires,Wants beat-preview-agent.service` 不再包含服务端；
再执行 `systemctl restart beat-preview-server.service` 实测 agent 保持 active（`ActiveEnterTimestamp` 未变），
新服务端 PID 立即收到 `/api/v1/nodes/report` 201。
另核对：agent 掉线 49 秒时 `/node/:id` 只显示 `Metrics delayed`，不会出现 `workspace.load_failed`，两者无因果关系。

**主题演练（2026-10-16，只读 + 预览库补装）**：预览库此前只有 Canvas / Operations / Slate，本轮把发布包内
其余五套（Aurora / Graphite / Paper / Pulse / Terminal）经 `POST /api/v1/admin/themes/upload`（AdminExpensive
预算：身份 1 req/s、burst 2，连续上传要串行退避）补装，8 套全部就位。随后逐套激活并打开
`/node/:id` 校验（激活需先 `POST /api/v1/auth/reauthenticate`）：七套声明式主题分别渲染
`cards / tiles / table / table / cards / table / cards` 布局与各自 palette 圆角（Aurora `#E3F7F5` 8px、
Canvas `#E7DFF7` 8px、Graphite `#DCE3E8` 0px、Operations `#D2E9E7` 4px、Paper `#E3D6C0` 0px、
Terminal `#D7E6E4` 2px、Slate `#DFEAFE` 6px），Pulse 是沙箱 iframe 脚本主题、按设计不挂
`[data-public-theme]` 容器，iframe 内渲染出 `SERVER MONITORING AND OPERATIONS DASHBOARD` 与实时节点数据；
8 套均为零失败请求、无 `workspace.*` 原始键，演练后已恢复 Slate。响应式：390/768/1440 三视口下 `/`、`/wallboard`、`/node/:id`、`/admin/metrics` 横向溢出均为 0。
离线：Service Worker 接管后断网重载 `/` 渲染缓存外壳 + 可重试提示，恢复联网自动回到实时。

## 2026-10-16 指标工作区看板行分组与折叠（Grafana Row）与 v48 部署

新增 `row` 面板类型（第 11 种），把 Grafana 的 Row 落成看板内的分组与折叠能力。

- 分组：行面板把它之后的面板归入同一组，直到出现下一个行面板（`lib/workspace-row.ts` 的
  `rowGroups`/`queryablePanels`）。
- 折叠：`row_collapsed`（缺省不折叠）；折叠后整组面板不渲染，行标题右侧显示该组面板数量
  （`components/metric-workspace/row-header.tsx`）。
- 折叠即停查：折叠行内的面板与行面板自身都不进入查询实例（`lib/workspace-repeat.ts` 改用
  `queryablePanels`），因此不再发 `/metrics/query`。
- 编辑体验：面板类型下拉末位提供「行」；切到行会清空查询与可视化字段，只保留标题、宽度与「默认折叠」，
  切回可视化类型补一条默认查询（`lib/workspace-panel-editor.ts`）。
- 落库与只读：后台点击行标题折叠写入看板定义（与 Grafana 相同，随看板保存）；公共节点页只做本地
  折叠（`use-public-metric-workspace` 的 `rowOverrides`），不回写服务端。
- 校验与导入导出：后端 `internal/model/dashboard_row.go` 接受默认/折叠/半宽，拒绝行面板上的可视化字段；
  `row` 与 `row_collapsed` 进入导入导出白名单，行面板必须空序列，非行面板携带 `row_collapsed` 判为无效。
- 五语言补齐 `workspace.row`、`workspace.row_description`、`workspace.row_collapsed`、`workspace.row_panels`、
  `workspace.toggle_row`，公共外壳词条同步。

**顺带修复**：壁板页节点卡 CPU 副值此前直拼原始浮点（例如 `0.8685885439658506`），现按指标格式化为
一位小数，与首页节点卡一致。

**关于 `workspace.load_failed` 的再次核对**：注入接口失败模拟时约 45 秒后显示本地化提示（zh-CN
「指标工作区无法加载。」+ 重试按钮），不是原始键名；本轮借 15:42:47–15:43:14 的一次 30 秒停服窗口复现，
同 IP 用户在 15:43:33 起已恢复正常 200 请求。仍可复现的形态仍是升级窗口内长期未刷新的旧标签页，刷新或
等退避自愈即可（v45 起已会自愈）。

验证：`npx vitest run` 206 文件 / 1151 用例全通过，`tsc -b` 退出 0，`oxlint src` 仅既有 warning，
`npm run build` 通过全部分片预算（公共图 gzip 239,377 B + 最大语言分片 13,343 B = 252,720 B / 256,000 B，
最大私有分片 `admin-catalogs` 487.61 kB / 600 KiB）；后端 `gofmt -l internal/ cmd/` 无输出、
`goimports-reviser ./internal/model/` 退出 0、`golangci-lint run ./internal/model/...` 0 issues、
`go build ./internal/... ./cmd/...` 与 `go test ./internal/model/` 通过。

实机走查（`http://127.0.0.1:8081`）：接口建含 `row` 的看板返回 201，行标题渲染「Row A · 2 个面板」、
组内面板为注释/CPU/Mem；展开态发起 6 条查询覆盖 cpu/memory，点击折叠后面板从 DOM 消失且 20 秒窗口内
零查询，再展开查询恢复；删除测试看板返回 204。公共节点页渲染行标题与计数，默认折叠的行不发查询。
`/`、`/node/:id`、`/wallboard`、`/admin/metrics` 与公网页面巡检无 `workspace.*` 原始键与失败请求。

**v48 部署**：`release/preview-v48-20261016`（后端与 Agent 均 `0.2.0-dev+worktree.20261016.10`，前端入口
`assets/index-tGIpT91O.js`）发布到 `/opt/beat-preview/releases/worktree-v48-20261016T082109Z`，冷备份
`/var/backups/beat-preview/pre-v48-20261016T082109Z`；包内 190 项 SHA256 校验通过，schema 仍为 v26
`agent_discovery`、integrity ok、外键 0 违规。Agent 上报版本随之变为 `0.2.0-dev+worktree.20261016.10`。

## 2026-10-16 指标工作区直方图面板、工作区文案分片与 v47 部署

新增 `histogram`（Grafana 的 Histogram 等价物），面板类型增至 10 种（timeseries/stat/gauge/table/
bargauge/piechart/barchart/heatmap/histogram/statetimeline）。

- 面板字段：`histogram_buckets`（2-64，缺省 12）、`histogram_scale`（`linear`/`log`）、
  `histogram_color`（缺省 blue，仅在合并序列时生效）、`histogram_merge`（缺省 true）。
- 校验：越界分桶、未知刻度与未知配色被拒绝；这些字段挂在非 histogram 面板上同样被拒绝
  （`internal/model/dashboard_histogram.go`）；直方图与热力图一样不接受值映射。
- 渲染：按 `[from, to)` 分桶、等于上界的样本归入最后一桶（与热力图共用 `lib/workspace-value-bins.ts` 的
  分桶数学）；每桶可命中 `data-histogram-bucket`/`data-histogram-bar`，计数轴 5 档，图例给出样本总数
  `workspace.histogram_samples`；合并序列时统一配色，未合并时按各序列颜色分别绘制。
- 五语言补齐 14 条 `workspace.histogram*` 词条，公共外壳词条同步，导入导出白名单与面板类型下拉同步接线。
- 分桶输入框改为只回显显式取值：清空后以缺省值 12 兜底，不再清空即回弹成 `12`（此前清空重填会变成
  `1224` 并触发「Value buckets must be an integer between 2 and 64」）。

**文案分片修正**：直方图新增词条把 `admin-catalogs` 顶到 604 KiB，超过 600 KiB 单分片门禁（v46 实测
599.09 KiB，余量不足 1 KiB）。指标工作区目录（五语言约 157 kB 源码）改为随 `metric-workspace` 与
`metrics/explore` 两个路由分片注册（与 `metric-explore` 目录既有的做法一致），`admin-catalogs` 回落到
487.61 kB（gzip 131.78 kB），后台其它页面不再下载这份目录。侧边栏渲染的 `workspace.title` 由新增的
`context/admin-shell-translations.ts` 常驻，并有单测守住它与工作区目录取值一致，避免出现原始键名。
公共外壳预算不变：公共图 gzip 239,320 B + 最大语言分片 13,305 B = 252,625 B / 256,000 B。

**关于用户报告的 `workspace.load_failed`**：本次排查未能在当前部署复现（`/`、`/node/:id`、
`/wallboard`、`/admin/metrics`、五语言、公网 `https://beat.xuthus.cc` 全部正常，服务端日志中用户浏览器
请求均为 200）。等价流程实测（页面前台打开 → 宿主机重跑升级脚本）不进入错误态，第 30 秒已恢复实时数据。
仍可复现的两种形态：访问不存在或已取消公开的节点 URL（显示「节点已不再公开或不可用」+ 通用错误提示），
以及长期未刷新、运行旧构建的标签页（v43 之前会把 `workspace.load_failed` 键名原样渲染，v45 之前退避预算
用尽后不自愈，必须手动刷新）。

验证（`http://127.0.0.1:8081` 无头浏览器）：后台「添加面板」→ 类型选「Histogram」→ 分桶填 6 → 应用后
渲染 `buckets=6 bars=6`、图例含 samples；`/admin/nodes`、`/admin/alerts`、`/admin/network`、
`/admin/metrics`、`/admin/metrics/explore` 巡检侧边栏文案正常且无任何 `workspace.*` 原始键；公共节点页
zh-CN 与 ja 各渲染 28 个图表元素、无失败请求；只有匿名 `GET /api/v1/auth/session` 的 401 属预期。

门禁：`npx vitest run` 206 文件 / 1151 用例全通过，`tsc -b` 退出 0，`oxlint src` 仅既有 warning，
`npm run build` 通过全部预算门禁；后端 `gofmt -l internal/ cmd/` 无输出、`goimports-reviser ./internal/model/`
退出 0、`golangci-lint run ./internal/model/...` 0 issues、`go build ./internal/... ./cmd/...` 与
`go test ./internal/model/` 通过。

**v47 部署**：`release/preview-v47-20261016`（后端与 Agent 均 `0.2.0-dev+worktree.20261016.9`，前端入口
`assets/index-DTjBgNHx.js`）发布到 `/opt/beat-preview/releases/worktree-v47-20261016T072812Z`，冷备份
`/var/backups/beat-preview/pre-v47-20261016T072812Z`；包内 190 项 SHA256 校验通过，schema 仍为 v26
`agent_discovery`、integrity ok、外键 0 违规。Agent 上报版本随之变为 `0.2.0-dev+worktree.20261016.9`。

## 2026-10-16 指标工作区状态时间线与 v46 部署

新增 `statetimeline`（Grafana 的 State timeline 等价物），用于展示节点、设备或任务在时间轴上的状态变化。

- 面板类型：`dashboardPanelTypes` 增加 `statetimeline`，现共 9 种（timeseries/stat/gauge/table/bargauge/piechart/barchart/heatmap/statetimeline）。
- 面板字段：`timeline_merge`（缺省 true，合并相邻同状态）与 `timeline_show_values`（`auto/always/never`，缺省 `auto`，控制块内取值标签）。
- 校验：未知取值模式被拒绝；非 statetimeline 面板出现这两个字段同样被拒绝（`internal/model/dashboard_state_timeline.go`）。
- 值映射：状态时间线支持值映射，把 `0/1/2` 命名为「正常/告警/未知」等；导入导出往返保留类型与字段。
- 渲染：每序列一行色块，缺样本留空；状态块超过 120 个时按等宽时间桶取桶内最后状态，避免长时段卡顿。
- 文案：中/英/繁/日/印尼五语言补齐 `workspace.state_timeline*` 词条，公共外壳词条同步。

验证（`http://127.0.0.1:8081` 与 `https://beat.xuthus.cc` 无头浏览器）：
- 后台「添加面板」→ 类型选「状态时间线」→ 选项区出现合并开关与取值标签下拉 → 应用后面板渲染 1 行 / 120 段，无控制台报错、无失败请求。
- 接口级：以 `statetimeline` 建看板返回 201 并回读字段一致，随后删除；`timeseries` 携带 timeline 字段与 `timeline_show_values=sometimes` 均返回 400。
- 巡检 `/`、`/wallboard`、`/node/:id`、`/admin/metrics`、`/admin/nodes`、`/admin/agent-setup` 无错误态、无 4xx/5xx。
- 前端门禁：`npx vitest run` 203 文件 / 1134 用例全通过；`tsc -b` 退出 0；`oxlint src` 仅既有 warning；`npm run build` 通过分片预算（公共图 gzip 239,344 B + 最大语言分片 13,296 B = 252,640 B / 256,000 B）。
- 后端门禁：`gofmt -l` 无输出、`goimports-reviser ./internal/model/` 退出 0、`golangci-lint run ./internal/model/...` 0 issues、`go test ./internal/model/` 通过。

**v46 部署**：`release/preview-v46-20261016`（后端与 Agent 均 `0.2.0-dev+worktree.20261016.8`，前端入口
`assets/index-9O4qfAh1.js`，Service Worker 版本 `e847b515f08009e9`）发布到
`/opt/beat-preview/releases/worktree-v46-20261016T064133Z`，冷备份
`/var/backups/beat-preview/pre-v46-20261016T064133Z`；包内 190 项 SHA256 校验通过，schema 仍为 v26
`agent_discovery`、integrity ok、外键 0 违规。Agent 上报版本随之变为 `0.2.0-dev+worktree.20261016.8`。

## 2026-10-16 指标工作区错误态自愈与 v45 部署

**现象**：用户在宿主机执行升级后访问页面报 `workspace.load_failed`。当前 v44 部署实测健康
（`/`、`/wallboard`、`/node/:id`、`/admin/*` 全部渲染，无 4xx/5xx，服务端重启窗口内页面零失败请求），
但复现出真正的缺陷：首屏接口失败时页面先按 `400/1000/2500/5000/8000/12000ms` 退避重试（合计约
29 s），预算用尽即进入错误态；**此后即使接口已恢复，页面也不会自愈**，必须手动点「重试」或刷新，
观感等同于「升级把页面打坏」。

**根因**：`usePublicWorkspaceSources` / `useWorkspaceSources` 把退避预算用尽当作终态；
`useErrorRecovery` 只在标签页重新可见、网络恢复或 bfcache 返回时重试，用户停在页面上时不会触发。
后台工作区的成功分支还漏了 `setError(null)`，恢复后错误横幅会一直挂着。

**修复**：
- `webui/src/lib/workspace-retry.ts` 新增 `workspaceRecoveryDelay`：退避预算用尽后按 20 s 间隔继续
  重试，最多 15 次（约 5 分钟），429 带 `Retry-After` 时按服务端提示等待（上限 30 s）；401/403/404
  等确定性失败仍立即落到错误态，不做无意义轮询。
- 两个工作区 Hook 在落到错误态后按上述节奏继续重试，成功后清掉错误态并渲染数据；后台 Hook 补上成功
  分支的错误清理。慢速预算用尽即停止，页面保留错误提示与「重试」入口。

验证（`http://127.0.0.1:8081` 与 `https://beat.xuthus.cc` 无头浏览器）：
- 公共节点页：注入 40 s 全接口失败 → 显示「指标工作区无法加载。」→ 恢复接口后 **10 s 内自动恢复**
  渲染（修复前 30 s 仍停在错误态）。
- 后台指标页：同样注入失败后恢复接口 → 5 s 内自动恢复，无残留错误横幅。
- 服务端 `systemctl restart` 期间已打开的页面零失败请求；冷启动、五语言路由 4xx/5xx 均为 0。
- `vitest run`：201 文件 / 1110 用例全通过（新增 `workspace-retry`、公共与后台工作区 Hook 用例）；
  `tsc -b` 退出 0；`oxlint src` 仅既有 warning；`npm run build` 通过分片预算
  （公共图 gzip 239,328 B + 最大语言分片 13,283 B = 252,611 B / 256,000 B）。

**v45 部署**：`release/preview-v45-20261016`（后端二进制与 v44 相同 `0.2.0-dev+worktree.20261016.7`，
入口 `assets/index-D9wBIO40.js`，Service Worker 版本 `da542f8079850bdc`）发布到
`/opt/beat-preview/releases/worktree-v45-20261016T060700Z`，冷备份
`/var/backups/beat-preview/pre-v45-20261016T060700Z`；包内 190 项 SHA256 校验通过，schema 仍为
v26 `agent_discovery`、integrity ok、外键 0 违规。

## 2026-10-16 指标工作区热力图与 v44 部署（补记）

新增 `heatmap` 面板类型：时间做列、取值分桶做行，单元格亮度表示样本数；`heatmap_buckets` 2..64
（缺省 12）、`heatmap_scale` linear/log（log 下界需大于零）、`heatmap_color` 取面板调色板；三项字段
只对热力图生效，其它类型带上会被服务端拒绝，导入导出往返保留。前端 `workspace-heatmap.ts` 计算分桶、
色标与 3000 单元格预算，`heatmap.tsx` 渲染并给出色标图例与空状态；五语言补 12 个
`workspace.heatmap*` 键，`workspace.heatmap_samples` 进入公共外壳键集。

验证：`vitest run` 199 文件 /1102 用例全通过；`tsc -b` 退出 0；`go test ./internal/model/`、
`golangci-lint run ./internal/model/`、`gofmt`/`goimports-reviser` 全部干净；`npm run build` 通过
分片预算（公共图 gzip 239,330 B + 最大语言分片 13,283 B = 252,613 B / 256,000 B）。部署于
`release/preview-v44-20261016` → `/opt/beat-preview/releases/worktree-v44-20261016T054553Z`，
冷备份 `/var/backups/beat-preview/pre-v44-20261016T054553Z`。宿主机浏览器上的热力图渲染仍未实测。


## 2026-10-16 公共节点页工作区文案补齐与 v43 部署

**现象**：v42 修好了失败提示，但公共节点详情页的控制条仍在显示原始键名——时间范围
`workspace.last` / `workspace.live`、刷新间隔 `workspace.seconds`、图表提示
`workspace.drag_zoom`、面板页脚 `workspace.last_success`。

**根因**：公共外壳按语言分片只带公共文案目录，管理端工作区目录
（`context/metric-workspace-translations.ts`，内联进 605 kB 的 `admin-catalogs` 分片）不在公共
路径内；而节点页复用的时间控件、图例、面板页脚都查 `workspace.*` 键，缺键时回退成键名。

**修复**：
- 新增 `webui/src/context/public-workspace/{en,zh-cn,zh-tw,ja,id}.ts`：只收录公共只读工作区会渲染的
  84 个键，值由工作区目录生成，随 `context/public-locales/*` 语言分片下发，英文随外壳内联；管理端
  仍使用完整目录，公共路由不会加载 `admin-catalogs`。
- 新增 `webui/src/context/public-workspace/catalog.test.ts`：校验五语言键集一致、逐键与管理端工作区
  目录相等、控制条关键键必须存在；`webui/src/test/catalogs.ts` 把五份子集并入目录门禁。

验证（`https://beat.xuthus.cc` 无头浏览器）：
- en、zh-CN、zh-TW、ja、id 五种语言的节点页均无 `workspace.*` 原始键，各渲染 28 个图表元素。
- 依次打开时间范围下拉、自定义时间弹窗、检查数据弹窗、放大查看弹窗后再扫描正文，仍无原始键。
- 注入 2.5s 全接口失败（模拟服务端重启窗口）：自动恢复渲染，无错误提示、无重试按钮。
- 体积门禁：公共图 239302 B + 最大语言分片 13271 B = 252573 B / 256000 B，仍在 250 KiB 预算内；
  `admin-catalogs` 仍为 605.59 kB。
- `vitest run`：197 文件 1084 项全部通过；`tsc -b` 退出 0。

**v43 部署**：`release/preview-v43-20261016`（同一后端 `0.2.0-dev+worktree.20261016.6`，前端入口
`assets/index-BCQBqBgE.js`）发布到 `/opt/beat-preview/releases/worktree-v43-20261016T052502Z`，
冷备份 `/var/backups/beat-preview/pre-v43-20261016T052502Z`；包内 190 项 SHA256 校验通过，
schema 仍为 v26、integrity ok、外键 0 违规。

## 2026-10-16 指标工作区瞬时失败自愈与 v41/v42 部署

**现象**：升级或重启服务端后，公共节点详情页会停在错误提示上，提示内容还是原始的
`workspace.load_failed` 键名；用户刷新前看不到图表。

**根因**：两处独立缺陷叠加。一是 `usePublicWorkspaceSources` / `useWorkspaceSources` 在首屏
四个接口任一失败后只重试一次（400ms），服务端重启窗口（数秒）内的失败会直接落到错误态，且
页面只能靠手动点「重试」或刷新恢复；二是公共节点页复用指标工作区组件，但公共外壳只带公共文案
目录，工作区目录只在管理端注册，因此错误提示与部分标签直接渲染键名。

**修复**：
- 新增 `webui/src/lib/workspace-retry.ts`：按 `400/1000/2500/5000/8000/12000ms` 退避重试，
  429 优先按 `Retry-After` 退避，4xx（除 408/425/429）立即失败；公开与后台工作区共用。
- 新增 `webui/src/hooks/use-error-recovery.ts`：错误态下标签页重新可见、网络恢复或从 bfcache
  返回时自动重试一次，页面不再需要手动刷新。
- 公共节点页错误提示改用公共外壳键 `node.metrics_failed` / `node.unavailable` /
  `node.summary_stale` 与 `app.retry`（en、zh-CN、zh-TW、ja、id 五语言齐备），不再漏出
  `workspace.*` 原始键；同时避免把 605 kB 的 `admin-catalogs` 分片带进公共节点路由。

验证（`https://beat.xuthus.cc` 无头浏览器，zh-CN）：
- 正常访问：节点工作区渲染 28 个图表元素，页面无 `workspace.*` 原始键。
- 注入 2.5s 全接口失败（模拟重启窗口）：7 次请求被拒后自动恢复渲染，无错误提示、无重试按钮。
- 注入 35s 全接口失败（超出重试预算）：页面显示「指标工作区无法加载。」+「重试」按钮，
  没有任何原始键名，行为有界不悬挂。
- `vitest run`：1081 项全部通过（含新增的退避策略与恢复用例）；`tsc -b`、`oxlint` 通过。

**v41/v42 部署**：`release/preview-v41-20261016`（`0.2.0-dev+worktree.20261016.6`，
入口 `assets/index-Y2bE05ZD.js`）与 `release/preview-v42-20261016`（同一后端，前端入口
`assets/index-DddEYiax.js`）先后发布到 `/opt/beat-preview/releases/worktree-v41-20261016T045851Z`
与 `worktree-v42-20261016T051113Z`，冷备份 `/var/backups/beat-preview/pre-v41-20261016T045851Z`、
`pre-v42-20261016T051113Z`；schema 仍为 v26、integrity ok、外键 0 违规。

**遗留处理**：当时已知的「公共节点页仍显示部分工作区原始键」已由 v43 的
`context/public-workspace` 子集解决，见上一节；公共外壳依旧不加载 `admin-catalogs`。

## 2026-10-16 主题资源路由装配修复与 v40 部署

**现象**：管理端主题卡片预览图全部破图；`preview_url`（`/themes/assets/<digest>/preview.png`）
返回 404，访问日志里这条请求的 `route` 是 `"/"`，也就是被 SPA 兜底接走了。

**根因**：`internal/api/router.go` 把 `GET /themes/assets/{digest}/{asset...}` 注册在内层
`r.mux` 上，而内层 mux 只由最外层挂载在 `/api/v1/` 下，该前缀永远匹配不到；`preview_url`
与沙箱运行时的 iframe 入口（`runtime: sandboxed` 的 `dist/index.html`）都走这条前缀，因此
主题预览和沙箱主题整体不可用。既有测试直接把内层 mux 当根 mux 断言，掩盖了装配错误。

**修复**：新增 `Router.MountThemeAssets`（配 `Router.ThemeAssetHandler`），由
`cmd/server/runtime_http.go` 在 `/api/v1/` 之外的最外层 mux 上挂载同一前缀；`router_theme_test.go`
改为按生产装配断言（外层 mux + SPA 兜底），并断言预览图 200 `image/png` + immutable 缓存、
兜底不被触发、未知摘要 404。编码后的 `%2e%2e` 穿越与点段请求分别返回 404 / 307，API 前缀不受影响。

验证：
- `curl /themes/assets/<active digest>/preview.png` → 200 `image/png` 72172 B（修复前 404）；
- 无头浏览器登录后台打开 `/admin/settings`，3 张已安装主题预览图全部 `complete=true`、
  1440×900、无失败请求（修复前全为 404）；
- `go test ./internal/api/ ./cmd/server/ ./internal/theme/`、`golangci-lint run`、
  `goimports-reviser -set-exit-status` 全部通过。

**v40 部署**：`release/preview-v40-20261016`（`0.2.0-dev+worktree.20261016.5`，入口
`assets/index-BIngKcKj.js`）发布到 `/opt/beat-preview/releases/worktree-v40-20261016T043947Z`，
冷备份 `/var/backups/beat-preview/pre-v40-20261016T043947Z`，schema v26、integrity ok、外键 0 违规。

沙箱运行时（`themes/pulse`，`runtime: sandboxed`）在隔离实例上完整验证过：另起一个
`127.0.0.1:8099` 临时服务（独立数据目录，`systemd-run` 托管，验证后停止并删除），上传并激活
Pulse 后，公开首页渲染出 iframe（`sandbox="allow-scripts"`，1232×720），主题自带的
`theme.css`/脚本生效，控制台无错误、无失败请求；入口 HTML 返回 200 `text/html` +
`CSP: sandbox allow-scripts` + `X-Frame-Options: SAMEORIGIN` + `Cache-Control: no-cache`。
预览实例本身没有安装该主题，避免改动你正在验收的主题集合。

## 2026-10-16 公共入口代理信任修复与 v39 部署

**现象**：经 `https://beat.xuthus.cc` 访问时，所有改变状态的请求（管理员登录、保存看板、
节点纳管等）返回 `403 origin is not allowed`；访问日志里的 `client_ip` 全是 Cloudflare 边缘
地址（`2400:cb00::/32` 等），入口准入的按 IP 令牌桶因此被所有公网访客共享。

**根因**：`middleware.SameOrigin` 用请求的有效来源（scheme + host）与浏览器 `Origin` 比对，
而 scheme 只在直连对端落在 `BEAT_TRUSTED_PROXIES` 内时才采用 `X-Forwarded-Proto`。预览实例没配
这个变量，Cloudflare 直接回源到本机 `:8081`，服务端于是认为自己是 `http`，浏览器发来的是
`https://beat.xuthus.cc`，同源判定失败；`X-Forwarded-For` 同样不被采纳，审计与限流都退化成边缘 IP。

**修复**：`/etc/beat-preview/server.env` 新增
`BEAT_TRUSTED_PROXIES=<Cloudflare ips-v4/ips-v6 全量 CIDR>,127.0.0.0/8,::1/128`
（备份 `server.env.bak-20261016T122813`），随后重启 `beat-preview-server.service` 与
`beat-preview-agent.service`。只信任真正的直连代理，回环地址单独放行本机反向代理。

验证（经 Cloudflare 实测）：

- `curl -D- https://beat.xuthus.cc/readyz` 出现 `strict-transport-security`（修复前没有）；
- `POST https://beat.xuthus.cc/api/v1/auth/login` 由 403 变为 200；
- 访问日志 `client_ip` 由 Cloudflare 边缘地址变为真实出口地址（本次实测 `134.185.119.110`）；
- 无头浏览器在 `https://beat.xuthus.cc` 走完“首页 → 点击节点”，节点工作区渲染 28 个图表元素，
  没有 `workspace.load_failed`、没有失败请求；控制台只剩 Cloudflare 注入 beacon 的 CSP 报错（既有已知项）；
- 同一浏览器登录后打开 `/admin/metrics` 与 `/admin/metrics/explore` 都能渲染工作区。

**v39 部署**：`release/preview-v39-20261016`（`0.2.0-dev+worktree.20261016.4`，commit
`14fae14+dirty`，入口 `assets/index-BIngKcKj.js`，前端与 v38 相同）经 `deploy-preview-8081.sh`
发布到 `/opt/beat-preview/releases/worktree-v39-20261016T043241Z`，冷备份
`/var/backups/beat-preview/pre-v39-20261016T043241Z`，schema v26、SQLite integrity ok、外键 0 违规。
相对 v38 只增加“客户端取消不再记 5xx”：请求方主动断开时访问日志与 HTTP 指标记 499 并带
`client_canceled=true`、`written_status=<原状态>`，真实内部错误仍是 500 + WARN。

未覆盖：499 分支只有定向单测（`internal/api/handler/client_canceled_test.go`、
`internal/api/middleware/middleware_test.go`）；用 `curl --max-time` 复现太短，没有真正触发服务端取消路径。

## 2026-10-16 v38 公共指标流准入与来源判定修复

`/api/v1/ws/metrics` 之前没有显式分类，落到入口准入的默认分支（`admin_read`），匿名公共
页面的实时流会和后台控制台抢同一个令牌桶；长时间连接还会长期占用在途额度。现在该路由与
`/api/v1/agents/ws` 一样不受 HTTP 准入控制，由 `internal/metricsfanout` 的握手速率、每 IP
连接数和在途租约管理。同时：

- 同源判定归一化大小写与默认端口，上游代理补写或省略 `:443` / `:80` 不再误判为跨站；
- 已配置的 `BEAT_PUBLIC_WS_ORIGINS` 变成同源之外的额外白名单，代理改写 `Host` 时仍可放行；
- 握手被拒返回 `metrics stream unavailable: <reason>`，WebSocket 升级请求的日志新增 `host`
  与 `origin` 字段；
- 前端实时流失败时在控制台输出 `[metrics-ws]` 的 close code、reason 与失败次数。

本次发布 `/opt/beat-preview/releases/worktree-v38-20261016T022328Z`（服务端与 Agent
`0.2.0-dev+worktree.20261016.3`，提交 `14fae14+dirty`，前端入口
`assets/index-BIngKcKj.js`），冷备份 `/var/backups/beat-preview/pre-v38-20261016T022328Z`，
schema v26、SQLite integrity ok、外键 0 违规。`/etc/beat-preview/server.env` 新增
`BEAT_PUBLIC_WS_ORIGINS=https://beat.xuthus.cc,http://192.168.1.48:8081,http://localhost:8081`，
`/api/v1/admin/settings/deployment` 显示 `ws_origin_mode=configured`、3 条来源。

验证证据：无头浏览器分别打开局域网首页与 `https://beat.xuthus.cc` 首页，两条通道
`ws://192.168.1.48:8081/api/v1/ws/metrics` 与 `wss://beat.xuthus.cc/api/v1/ws/metrics`
均成功握手并持续收到帧，控制台无 WebSocket 失败；节点详情页在两条入口都渲染 8 个图表且没有
`workspace.load_failed`；注入一次连接级失败后控制台出现带 `code=1006` 的 `[metrics-ws]`
诊断行。curl 复核：同源、显式 `:443`、代理改写 `Host` 且来源在白名单内均为 `101`，未列入
来源返回 `403 metrics stream unavailable: origin`，无 `Origin` 的非浏览器客户端仍为 `101`；
连续三次握手不再改变任何入口类别计数。脚本与截图位于 `/tmp/beat-verify/`。

已知非本次范围的现象：Cloudflare 会注入 Web Analytics 的 `beacon.min.js`，被站点自身的
`default-src 'self'` CSP 拦下，控制台会出现一条与之相关的 CSP 报错。它与 WebSocket 无关，
处理方式是在 Cloudflare 后台关闭自动注入（Web Analytics / Rocket Loader），或者由运营方
自行决定是否把该第三方脚本写入 CSP。

停止预览：

```bash
systemctl stop beat-preview-agent.service beat-preview-server.service
```

重新启动：

```bash
systemctl start beat-preview-server.service beat-preview-agent.service
```

构建、前端测试及浏览器证据保留在 `/tmp/beat-resume-oct07/preview-*`。

## v12 更新与回退

2026-10-07 已经冷备份并由 systemd 重启到 v12。当时目录为
`/opt/beat-preview/current` → `/opt/beat-preview/releases/v12-20261007`；旧版本
保留在 `/opt/beat-preview/releases/v10-before-v12-20261007`。备份位于
`/var/backups/beat-preview/pre-v12-20261007`（状态与私有配置归档、SHA256SUMS、验证记录）。
迁移后 SQLite integrity_check=ok，节点凭据、管理员凭据和主题指纹与冷备份一致，
Agent 在线且 telemetry=fresh。回退必须先停止预览两个服务，再恢复匹配的状态归档
和旧版本目录；不能让旧版本直接打开 v12 数据库。

## v13 更新与回退

2026-10-07 固定候选通过后端及前端完整门禁后，预览由 systemd 更新到
`/opt/beat-preview/releases/v13-20261007`。冷备份位于
`/var/backups/beat-preview/pre-v13-20261007`，包含状态、私有配置、校验和与迁移验证。
SQLite schema13/integrity、原节点/管理员凭据及主题指纹均验证一致；Agent 已上报 v13，
在线且 telemetry=fresh。恢复时停止两个预览服务并还原匹配状态与 v12 程序。

新增体验入口：公共首页分组/筛选/墙板，后台 `/admin/lifecycle` 的国家与节点生命周期管理。
GeoIP 外部服务默认关闭；公开国家、到期和价格字段仍需明确启用。

## v14 更新与回退

2026-10-08 固定候选 `/tmp/beat-v14-candidate-r2-g203rjm4` 的 1,922 个源码文件
校验一致后，冷备份并通过 systemd 部署到 `/opt/beat-preview/releases/v14-20261008`。
当时链接 `/opt/beat-preview/current` 指向该目录。迁移至 schema14、完整性检查、
管理员及节点凭据/主题指纹保留均通过；Agent 在线且持续上报 fresh 数据。
IPv4 局域网、IPv4 回环和 IPv6 回环 readiness 均通过；两个服务无自动重启。

冷备份：`/var/backups/beat-preview/pre-v14-20261008`，包含状态和配置归档、
SHA256SUMS、原版本链接及迁移验证记录。需要回退时先停止两个预览服务，恢复匹配
状态和配置归档，再切回 v13 程序并启动；不能让 v13 直接打开 v14 数据库。

验证记录：后端完整门禁通过（race/shuffle、全局覆盖率 >=90%、格式/lint/vet、
依赖/漏洞检查与构建）；前端 120 文件/598 测试通过，行覆盖率 93.39%；脚本 37 测试
及覆盖率门禁通过。脚本总入口因快照没有 `.git` 未通过自动定位，随后以显式工作流
路径执行 actionlint 成功。Podman 的 Docker 格式镜像通过非 root、只读程序、零
capability、no-new-privileges、readiness 和 HEALTHCHECK 检查；未实测 Docker Engine。

浏览器实测首页、指标、生命周期、节点接入、告警、节点详情、GPU 库存和新节点默认
策略，零页面异常及失败 API 请求。日志和截图分别位于
`/tmp/beat-resume-oct07/v14-r2-*-gate.log`、`v14-container-evidence/`、
`v14-preview-evidence/`。本次临时容器、镜像、挂载目录和浏览器会话已清理。

## 2026-10-09 页面跳转修复

前后台共用的路由加载器在 React transition 重试时重新创建 lazy 组件，导致地址
变化但内容仍停留在旧页面。现在按稳定的 loader 缓存组件身份，显式重试时才替换；
切换页面同时重置对应的错误边界。

修复前新增的三项回归测试均失败，修复后通过；完整前端门禁通过，121 文件/601 测试，
行覆盖率 93.40%，路由加载器行覆盖率 100%。真实浏览器验证前后台互跳、节点详情
往返及前进/后退，连续十个检查点均未整页刷新，零页面异常和失败请求。

从固定 v14 候选仅替换加载器并添加回归测试，部署到 `v14-navfix-20261009`。
Server/Agent 二进制与原 v14 完全一致，进程未重启，schema 仍为 14；保留旧哈希资源。
仅回退此导航修复时，将 current 原子切回 `v14-20261008` 即可，无需数据库回退。
证据和回退目标位于 `/tmp/beat-navigation-fix/{browser-evidence,deployment}.json`，
集中门禁日志为同目录的 `frontend-gate.log`。其余功能开发仍暂停。

当时最终运行检查另发现 `/readyz` 返回 503，`mts=error/unavailable`，其余检查为 ok；
节点仍为 online/fresh，两个服务保持运行。当前探针不提供底层健康原因，尚未定位
根因。本次未重启服务或改变后端/数据库来掩盖状态；现场记录位于
`/tmp/beat-navigation-fix/runtime-check.json`，留待独立排查，不能将导航验证称为整体
运行健康验收。

## 2026-10-09 MTS 整理修复

后续通过认证维护接口取得健康原因，并只读核对磁盘清单：17 个分片存在重叠文件，
上游默认关闭自动整理，因此持续报告 `compaction degraded`。修复在刷盘时启用整理，
限制整理并发为 1；完整整理仍复用原有互斥维护流程，不启用独立后台定时器。

固定候选 `/tmp/beat-v14-compaction-final-b0gfmu9_` 仅修改 MTS 初始化并增加两项回归测试。
回归在修复前失败、修复后通过，覆盖新写入、旧重叠分片、重复时间点最新值及重启历史。
完整后端门禁通过：race/shuffle、全局覆盖率 >=90%、格式、lint、vet、模块验证、
可达漏洞扫描和构建。更早包含独立后台定时器的候选已放弃，其门禁被终止，不计为通过。

冷备份位于 `/var/backups/beat-preview/pre-v14-mtsfix-20261009`；通过 systemd 更新至
`v14-mtsfix-20261009`，schema 保持 14，SQLite integrity_check=ok。
管理员/节点凭据及主题指纹一致；固定半小时窗口内 CPU 和内存各 359 个点的 SHA256
保持一致。维护接口现为健康、无整理告警，Agent 已有重启后的新上报。
连续 8 次检查均 ready，捕获 8 个不同的新样本；浏览器十个前后台导航检查点均通过，
无整页刷新、页面异常或失败请求。本轮验证产物已清理，日志和证据文件保留。

本次回退需停止两个预览服务，恢复该冷备份的匹配状态与配置，再切回
`v14-navfix-20261009`；该旧版本仍有已知整理缺陷。日志、只读诊断、前后历史校验和
浏览器证据位于 `/tmp/beat-mts-diagnosis/`，部署证据同时保存在冷备份的 verification.json。

## 2026-10-14 页面跳转复核

再次收到“点击前后台链接后地址变化、内容停留在旧页面，手动刷新才更新”的反馈后复核：

- 该现象对应的根因已于 2026-10-09 修复：共用路由加载器在 React transition 重试时
  重新创建 lazy 组件。当前 `webui/src/components/route-chunk.tsx` 按 loader 稳定缓存
  组件身份，仅在显式重试时替换，并重置对应错误边界。
- 回归覆盖：`webui/src/components/route-navigation.test.tsx` 三项在旧写法下两项失败、
  修复后全绿；新增 `webui/src/pages/app-navigation.test.tsx` 覆盖“前台 → 后台 →
  `/admin/lifecycle` → `/admin/groups` → 前台”的真实路由往返，断言地址与内容同步更新。
- 浏览器缓存或发布目录不会自动更新。运行实例按 `-static-dir` 读取各自前端副本：
  `beat-server.service`（:8080）指向 `/opt/beat/current/static`，仍是 2026-09-13 构建，
  不含该修复；8081 预览的 `v14-mtsfix-20261009` 已包含。工作树 `webui/dist` 已在
  2026-10-14 重建，入口哈希随每次构建变化，可用 `webui/dist/index.html` 中的
  `/assets/index-*.js` 与浏览器开发者工具加载的脚本比对。
- 让浏览器拿到修复需要两步：更新实例的 static 目录，然后在浏览器硬刷新一次。本沙箱
  无法写入 `/opt`，也无法启动 Chromium（`sandbox_host_linux.cc` 致命错误），因此本次
  未做实机浏览器验证，只完成源码、构建产物与回归测试层面的确认。

同类缺陷收口（2026-10-14）：`public-network-history.tsx` 仍在组件内用
`useMemo(() => lazy(load), [])` 重建 lazy，重挂载会重新挂起。稳定分片原语已提取到
`webui/src/components/route-chunk-cache.ts`（`stableChunk` / `resetChunk`），路由与
网络历史弹窗共用；新增反向验证（去掉缓存后 `route-chunk.test.tsx` 两项失败，恢复后
全绿）。定向 6 个测试文件 24 项通过，`tsc -b`、`oxlint` 与 `npm run build` 通过，
当时入口哈希为 `/assets/index-gnNHPfjD.js`；加入服务主体审计标识后的当前工作树入口为
`/assets/index-B4ie4IYp.js`。预览实例前端仍是 2026-10-09 的 v14 构建，
未更新：当前工作树前端包含 v15/v16 界面，与 v14 后端混用不在验收范围内。

### 2026-10-14 二次复核（更正上一节结论）

上一节把“8080 静态目录不含修复”当作现象成因，本次复核证明该推断不成立，并补上当前证据：

- 2026-09-13 发布目录 `agent-join-20260913T025921Z` 的 static 为**单包前端**（`index-DE7vMPRD.js`，1.56 MB，
  与发布目录 `SHA256SUMS` 中 `22a05b41…` 一致；`/wallboard`、`/lifecycle`、`/accelerators` 均不存在，
  全包无 `lazy(` / 路由级 Suspense）。没有代码分割就**无法产生**本现象，因此“用户看到 8080 旧包”不是成因。
- 运行实例的访问日志（journal）显示最近一次浏览器请求：`beat-server.service`（:8080）
  为 2026-10-07 11:29（`192.168.1.6`，随后 `/api/v1/admin/theme-markets`、`/api/v1/ws/metrics` 等）；
  `beat-preview-server.service`（:8081）为 2026-10-09 21:10。两实例此后都只有 Agent 上报，
  说明“点击链接不跳转”不是这两台实例**新加载**页面产生的，而来自长期未刷新的标签页/PWA
  或本机 journal 视野之外的实例。
- 当前工作树构建复核：`webui/dist` 重建后入口仍是 `/assets/index-B4ie4IYp.js`，构建可复现；
  入口分片内含稳定缓存实现（`new WeakMap` 缓存 `{Page: lazy(load), key}`，仅显式重试时 `delete`）。
- 回归复核（2026-10-14）：用**真实页面模块 + 未包 `act()` 的原生点击 + 真实定时器**走完
  2026-10-09 复现用的失败路径 `前台 → /admin → nodes → metrics → lifecycle → accelerators → nodes`，
  每一步地址与内容都同步提交，无整页刷新。2026-10-09 的浏览器复现与修复后证据仍在
  `/tmp/beat-navigation-fix/`（`reproduction.json`、`browser-evidence.json`、截图）。
- 定位建议：在目标页执行 `performance.getEntriesByType('resource').filter(r=>/assets\/index-.*\.js/.test(r.name)).map(r=>r.name)`，
  与 `webui/dist/index.html` 中的入口哈希比对；不一致即为旧包，硬刷新一次；若是已安装 PWA，
  在 DevTools → Application 中注销 Service Worker 并清空站点数据。
- 本沙箱限制（已实测）：根文件系统与 `/opt` 只读（`touch /opt/beat/...` 返回 read-only file system），
  systemd 总线不可用，TCP `listen` 返回 `EPERM`，Chromium（1193/1228/1234 与 headless shell）在 crashpad
  `setsockopt EPERM` 后 SIGTRAP 退出。因此**发布与实机浏览器验证必须在宿主机执行**，沙箱内只能
  完成源码、构建产物、日志与无头 DOM 层面的确认。

### 2026-10-14 三次复核（定位到"旧页面"而非服务端）

本轮用发布目录、journal 与历史构建日志核对了两台实例实际下发的入口包：

- `beat-server.service`（:8080）入口为 `/assets/index-DE7vMPRD.js`（2026-09-13 单包前端，
  无 `lazy(`/路由级 Suspense），journal 中最后一次浏览器请求是 2026-10-07 11:29。
- `beat-preview-server.service`（:8081）入口为 `/assets/index-B31OmizM.js`，与
  `/tmp/beat-navigation-fix/build.log` 里 2026-10-09 导航修复构建的入口哈希一致；
  journal 中最后一次浏览器请求是 2026-10-09 21:10（此后只有 Agent 上报）。
- 结论：现象只可能来自"点击时仍在内存中的旧页面"，而不是当前部署。旧页面的路由分片
  在发布后被替换会 404，于是地址变化、内容不动，必须手动刷新。

收口改动：`webui/src/lib/chunk-recovery.ts` 在首次 `vite:preloadError` 时自动整页加载
一次（30 秒窗口 + sessionStorage/内存双保险，避免刷新循环），`webui/src/main.tsx`
注册该监听；重复失败仍交给 `ChunkBoundary` 的手动重试。定向 7 项测试通过，完整前端
130 文件/640 测试、行覆盖率 93.41% 通过（`editors.test.tsx` 在默认 5 秒超时的并行
满载下会偶发超时，单文件与外层 20 秒超时均通过），`tsc -b`、`oxlint`、`npm run build`
通过，新入口为 `/assets/index-DIvKqCGY.js`。沙箱无法写 `/opt`，部署与实机浏览器复核
仍需在宿主机执行。

### 2026-10-14 补充复核（流量证据与工作树门禁）

- 宿主机两个实例都在运行（`beat-server.service` 为 :8080、`beat-preview-server.service` 为 :8081，
  后者 PID 自 2026-10-09 起未变）。`journalctl --since 2026-10-10` 全量统计只有
  `POST /api/v1/nodes/report`（154273 次）与 `GET /api/v1/network/assignments`（25740 次），
  来源仅 `::1` / `127.0.0.1`，没有任何浏览器请求或静态资源请求。即该轮看到的失效页面
  不是新加载的页面，符合上面"旧页面"的结论。
- 当前工作树门禁（2026-10-14 11:15 复跑）：`npx vitest run` 133 文件 / 657 测试通过；
  `tsc -b` 与 `oxlint` 通过；构建入口 `/assets/index-BqsMZIrq.js`，包内含
  `beat:chunk-recovery-at` 与 `vite:preloadError` 监听；`dist/themes/*.theme.zip` 与
  `public/themes/` 的 SHA256 一致，构建产物与源码同步。
- 本沙箱仍无法落地：`/opt` 只读（`Errno 30`）、`systemctl` 连不上 system scope 总线、
  TCP `listen` 返回 EPERM。把新前端交付到实例必须在宿主机执行。

宿主机落地命令（以预览实例 :8081 为例；服务以 `beat-preview` 运行，需要同时改属主）：

```bash
sudo cp -a /opt/beat-preview/current/static "/var/backups/beat/static-$(date +%Y%m%dT%H%M%S)"
sudo cp -a /root/projects/beat/webui/dist/. /opt/beat-preview/current/static/
sudo chown -R beat-preview:beat-preview /opt/beat-preview/current/static
curl -s http://127.0.0.1:8081/ | grep -o '/assets/index-[A-Za-z0-9_-]*\.js'
grep -o '/assets/index-[A-Za-z0-9_-]*\.js' /root/projects/beat/webui/dist/index.html
```

两条入口哈希一致后再在浏览器硬刷新一次（Ctrl+Shift+R）；若该站点装过 PWA，需在
DevTools → Application 注销 Service Worker 并清空站点数据。`cp -a` 不删除旧 hash 资源，
已打开的页面仍能跑完未完成的资源请求。不要把当前工作树前端（v15/v16 界面）直接覆盖到
`/opt/beat/current`（v14 后端），版本混用不在验收范围内；:8080 需要按同一版本重建整包发布。

### 2026-10-14 旧标签页自愈（版本巡检）

现象复现链已闭环：用户浏览器最后一次**修复前**入口加载是 2026-10-09 08:11
（`/assets/index-Ch-lloUw.js`），09:57 前后已换成修复包 `index-B31OmizM.js`；此后两台实例
journal 再没有任何页面请求。也就是说“点击链接不跳转”只可能来自仍然驻留内存的旧文档，
而 `:8080`（v7 单包，无 `vite preload`/路由级 Suspense）结构上无法产生该现象。

为让这类旧文档不再需要人工刷新，新增 `webui/src/lib/version-watch.ts`：页面可见时
拉取 `/index.html`（`no-store`）比对入口分片，发现新构建就整页加载一次；同一入口只触发
一次（sessionStorage + 内存双记录），解析/网络失败一律保持当前页面。该模块由
`main.tsx` 通过动态 `import()` 按需加载（构建产物 `version-watch-*.js`，约 0.6 KiB gzip），
不占用公共外壳 250 KiB 预算。

验证：`version-watch` 9 项、路由/分片 25 项定向通过；完整前端 134 文件全部通过
（行覆盖 93.56%，语句 90.76%）；`tsc -b`、`oxlint`、`vite build` 通过，入口
`/assets/index-dt309-xe.js`，构建产物含 `version-watch` 分片。

复现观察方式：目标页执行
`performance.getEntriesByType('resource').filter(r=>/assets\/index-.*\.js/.test(r.name)).map(r=>r.name)`，
与 `webui/dist/index.html` 的入口哈希比对；不一致即旧包，硬刷新一次即可，之后由版本巡检
自动收敛。

### 2026-10-14 第四次复核（日志证据：失效页面是 10-09 留在内存里的修复前文档）

本沙箱无网络、无浏览器，改用 `/var/log/messages` 与 `/var/log/messages-20261011` 中两实例的
访问日志复核（每条 `http request` 都带 `client_ip`、`path`、`status`）：

- 用户浏览器出口是 IPv6 `2408:8459:…`，全程只访问 `beat-preview-server`（:8081）：
  `2026-10-09 08:11:01 GET /` 下发入口 `index-Ch-lloUw.js`（**导航修复前**的包），
  `08:57:54 GET /` 下发入口 `index-B31OmizM.js`（`v14-navfix-20261009` 修复包，
  与 `/tmp/beat-navigation-fix/build.log` 一致）。此后该地址再无页面请求，
  最后一次任意请求是 `2026-10-09 10:01:26`（客户端路由触发的 `auth/session`、`groups`、
  `custom-metrics`、`nodes`），10-11 之后两台实例只剩 Agent 上报，也没有 nginx/caddy/traefik 记录。
- 10-09 的部署证据（`/tmp/beat-navigation-fix/deployment.json`）显示发布时**保留了 120 个旧
  哈希分片**，同一份证据里真实浏览器十个检查点全部 `same_document: true`、无报错、无失败请求。
  也就是说 10-09 之后旧分片不会 404，用户看到的“地址变了、内容不动”来自仍在内存里的
  **修复前文档**（`index-Ch-lloUw.js`）——它的 lazy 身份在 transition 重试时被重建，
  反复挂起且不产生网络请求，正好对应日志里“点击之后没有任何请求”。
- 预览实例在 `2026-10-09 10:46:18` 重启为 `0.2.0-dev.v14.1`（PID 1872224），
  `beat-server.service`（:8080，PID 1796472）仍是 2026-09-13 单包 `index-DE7vMPRD.js`，
  且全程没有用户浏览器请求，结构上也没有路由级懒加载。

结论与落地动作：

1. 旧文档只需要一次硬刷新（Ctrl+Shift+R）即可收敛：硬刷新后拿到的是已修复的 10-09 v14.1 前端；
   若站点装过 PWA，还要在 DevTools → Application 注销 Service Worker 并清空站点数据。
2. 发布时继续保留上一版 `static/assets`（`cp -a dist/. <static>/` 是覆盖式合并，旧分片仍在；
   `beatctl.sh` 走新 release 目录，需要显式合并上一版 `static/assets`），否则旧标签页会从
   “反复挂起”退化为“分片 404”，两种表现一样需要人工刷新。
3. 之后按同一版本整包发布工作树前端+后端，`version-watch` 会在发布后自动整页加载新版本，
   不再需要人工刷新；`chunk-recovery` 兜住分片 404 这一类。

本轮工作树验证（2026-10-14 13:39）：`npx vitest run` 136 文件 / 672 测试通过；`tsc -b`、`oxlint`、
`npm run build` 通过；入口 `/assets/index-dt309-xe.js`，入口含 `beat:chunk-recovery-at`，
按需分片 `version-watch-*.js` 含 `beat:version-reloaded`。同时修掉上一轮遗留导致 `tsc -b` 失败的
两个测试文件：删除一次性探针 `src/lib/zz-probe.test.ts`，把主题示例测试移到
`webui/scripts/theme-example.test.ts` 并改为在 jsdom 中执行已发布的 Pulse 主题源码
（`src/` 不带 Node 类型，原位置无法通过类型门禁）。

### 2026-10-14 分片按模块复用（点击链接不跳转的回归防线）

用户再次反馈“点击链接不跳转、必须手动刷新”。本轮复核结论与上一轮一致，并补上结构性防线：

- 两台在跑实例的前端都不含该缺陷。`:8080` 的 `beat-server.service` 仍指向
  `/opt/beat/current/static/assets/index-DE7vMPRD.js`（2026-09-13 单包，全仓库唯一 JS，
  包内 `lazy(` 出现 0 次，路由全部静态导入）；`:8081` 是 `v14-navfix-20261009` 之后的修复包，
  其入口 `index-B31OmizM.js` 与 `/tmp/beat-navigation-fix/build.log` 一致。
- 失效页面只可能来自浏览器里驻留的修复前文档（`index-Ch-lloUw.js`，2026-10-09 08:11 载入）：
  它的路由元素每次渲染都新建 loader，transition 重试会重建 lazy 身份并再次挂起，
  点击时既不改变内容也不产生任何网络请求，只能硬刷新恢复。
- 为让这一写法不再能造成挂起，`webui/src/components/route-chunk-cache.ts` 改为
  **按模块复用分片**：源码是无参、无插值、`import()` 写字符串字面量的 loader 按源码文本命中同一条目，
  其余（动态拼接、带闭包）仍按函数身份区分。`resetChunk` 按同一键清理，
  重试路径不受影响。三个新用例见 `webui/src/components/route-chunk.test.tsx`；
  反向验证：把 `stableChunk` 退回“只按函数身份”后，共享用例与重建用例立即失败。

本轮工作树验证（2026-10-14 14:09）：`npx vitest run` 136 文件 / 675 用例通过，
行覆盖 93.56%（`route-chunk.tsx` 100%）；`tsc -b`、`oxlint`、`vite build` 通过，
入口 `/assets/index-BZOJcuuy.js`。

2026-10-15 更新：公共外壳贴顶问题已通过拆包解决——仅管理端使用的文案目录从共享的
`locale.tsx` 移到 `webui/src/context/admin-catalogs.ts`，由管理端分片与测试
setup 注册。公共图 gzip 由 256,175 B 降到 229,727 B（限 256,000 B），余量约 26 KB，
预算上限未调整。同一轮把主题构建脚本接入门禁：`scripts/check.sh themes`（`bash -n`
+ `go test ./cmd/theme/...`，CI 部署 job 执行），`all` 也包含该阶段。

⚠️ 公共外壳预算长期贴顶：`vite build` 的 `beat-public-shell` 守卫上限是 250 KiB = 256000 字节。
2026-10-14 复算为 255279 字节（余量约 0.7 KB）——余量来自把只服务指标工作台的文案移出全局字典
（见下一节），不是预算放宽。结构上仍建议二选一：把公共面专用代码（如 PWA 面板）改为按需动态导入，
或经所有者同意后上调该预算；两项都属根配置/公共面改动，需明确批准。

### 2026-10-14 指标看板图例取值/排序/提示模式

对齐 Grafana 的面板级图例选项，只改看板 JSON 定义，不涉及 `v25` 迁移：

- `internal/model/dashboard.go` 的 `DashboardPanel` 新增 `legend_values`、`legend_sort`、`tooltip`
  三个 `omitempty` 字段；`dashboard_validation.go` 校验取值（`last/min/max/mean`，最多 4 项、不得重复）、
  排序（`none/asc/desc`）、提示框（`all/single/off`），空字符串等价默认 `none`/`all`，
  图例隐藏时禁止带取值或非默认排序。
- `WorkspaceLegend`（`components/metric-workspace/legend.tsx`）在图例有取值时渲染表格
  （首列系列名 + 各取值列，缺失值排最后），无取值时保持原来的 flex 行内图例；
  `chart.tsx` 的 `Tooltip` 按 `panel.tooltip` 切换 `shared` 或整块隐藏。
- 面板选项新增取值开关、排序与提示框下拉，图例关闭时同步清空 `legend_values`/`legend_sort`；
  导出/导入（`workspace-portable.ts`）对三个字段做往返与非法值收敛。
- ⚠️ 这些文案原本写进 `src/context/metric-workspace-translations.ts`，把公共外壳推到
  `PUBLIC_GZIP=256239`，`vite build` 直接失败——该字典随入口分片进入公共外壳，
  而图例文案只服务指标工作台。按 `src/pages/fleet-text.ts` 的既有写法移入
  `src/components/metric-workspace/legend-text.ts` 后，公共外壳回到预算内。
- 本轮验证（2026-10-14 14:27）：`npx vitest run` 137 文件 / 678 用例通过；`--coverage` 行覆盖 93.24%；
  `tsc -b`、`oxlint`、`npm run build` 通过，入口 `assets/index-3p6F4HuQ.js`。
- 补测已落地（2026-10-14 14:35）：`legend.test.tsx` 覆盖表格渲染、无取值时的行内图例、按首个取值排序、
  点击系列名隐藏/恢复；`editors.test.tsx` 覆盖取值开关、排序、提示框与关闭图例清空；
  `workspace-portable.test.ts` 覆盖往返、默认值省略与越界/重复/隐藏图例的非法组合。
  全量 `npx vitest run` 138 文件 / 685 用例通过，行覆盖 93.66%。

### 2026-10-14 收口（访问日志时间线 + 反向验证）

再次复核“点击链接不跳转、需手动刷新”，以两台实例的访问日志时间线定案，不再依赖推断：

- 预览实例（`beat-server[1872224]`，:8081）在 2026-10-09 10:48:33 的浏览器会话加载的是
  **修复后**入口 `/assets/index-B31OmizM.js`；同一次会话里 `/admin/accelerators` → 指标工作台 →
  `/admin/lifecycle` → 前台仪表盘 → 节点详情 → `/admin/groups` 逐页点开，每页的分片
  （`admin-`、`metrics-`、`lifecycle-`、`dashboard-`、`node-detail-`、`groups-` 等）与页面级 API
  （`metrics/query`、`nodes`、`nodes/{id}`、`groups`）都真实到达，说明修复后的构建点击即跳转。
- 修复前入口 `/assets/index-Ch-lloUw.js` 的最后一次下发是 2026-10-09 08:17:30；08:11 与 08:17
  两次页面加载都可能成为长期驻留的标签页。此后到 2026-10-14 16:17，两台实例**没有任何浏览器请求**
  （今天只有 `127.0.0.1`/`::1` 的 Agent 上报），因此当前反馈仍来自修复前文档。
- 反向验证（本工作树）：把 `webui/src/components/route-chunk-cache.ts` 的 `stableChunk` 退回
  “每次新建 `lazy`”，`route-navigation.test.tsx` 3 项中 2 项失败；还原后（sha256 一致）
  路由/分片/应用级导航 3 文件 14 项全绿。缓存对导航语义是必需的，不是可选优化。
- 工作树前端门禁（2026-10-14 16:2x）：139 文件 / 692 用例通过，行覆盖 93.67%，`tsc -b`、
  `oxlint`、`npm run build` 通过，入口 `/assets/index-DrT5HIdV.js`（`index.html` 已同步）。
- 落地仍需宿主执行：`/opt/beat-preview` 属主 `nobody`、权限 0700，本沙箱读不到也写不了（`Errno 13`），
  TCP `listen` 与 `systemctl` 同样不可用；把新前端交付到实例按上面的 `sudo cp -a` 三条命令执行。

### 2026-10-14 v17 登录安全事件控制台

`/admin/security` 新增“登录安全事件”页签（排在页签最左，默认页签仍是账户，未改变既有落地视图）：

- `webui/src/pages/admin/security-events-panel.tsx`：事件表 + 类型/级别/原因/来源/投递筛选 + 分页 +
  出现失败投递时的提示；轮询为页面可见时 15 秒静默刷新，不重置筛选、滚动位置与已打开的详情。
- `webui/src/pages/admin/security-event-detail.tsx`：Sheet 详情，展示脱敏来源/设备摘要、原因、
  路由状态与投递行（渠道名、状态、尝试次数、错误码）；确认后同时刷新列表与详情。
- `webui/src/pages/admin/security-policy-editor.tsx`：仅 owner 可见，含事件订阅、隐私级别、
  回溯天数、锁定阈值/窗口、动态码阈值、保留天数与发送内容预览；保存走最近身份确认与审计。
- `webui/src/pages/admin/security-events-text.ts`：控制台文案与词表标签留在后台分片，
  公共外壳 gzip 仅 +5 B（254535 → 254540，预算 256000）。
- DOM 只渲染标签与归一化摘要，机器值（事件 ID、actor ID、message ID、reason 机器值）不出现在页面。

本轮门禁（2026-10-14 18:0x）：`tsc -b`、`oxlint`（仅既有警告）、`npx vitest run` 140 文件 / 702 用例、
行覆盖 93.41%、`npm run build` 通过（公共外壳 254540 B），入口 `/assets/index-BcLG94Ex.js`。
控制台定向测试 `webui/src/pages/admin/security-events-panel.test.tsx` 10 项覆盖筛选、详情、
投递失败提示、确认、owner/admin 权限、隐私预览、中文文案与失败路径。

### 2026-10-14 v17 来源揭示收口（owner-only reveal）

- 后端新增 `GET /api/v1/admin/security/events/{id}/source`（`recentAuthOwnerAdmin`）：
  先 `RequireOwnerRecent`，再要求策略 `privacy_level = full_source`，读取与写
  `security.event.source_reveal` 审计在同一事务内提交（`internal/store/security_event_source.go`）。
- 错误语义按事件是否存在区分：事件不存在返回 `404`，事件存在但没有受保护来源记录返回
  `409 no protected source record for this event`，coarse 隐私返回 `403`。
  该区分由 `securityEventSourceMiss` 实现，避免删除后的事件被误报为“缺少受保护记录”。
- 新增 handler 用例 `internal/api/handler/admin_security_events_test.go`（3 项）：未认证 `401`、
  过期会话 `428`、admin 角色与 coarse 隐私 `403`、链接审计后 `200`（含地址与记录时间，
  并核对审计行 actor/resource）、无链接 `409` 与未知事件 `404`。`internal/store/security_event_source_test.go`
  增补空地址事件用例，并把未知事件断言改为 `ErrSecurityEventNotFound`。
- 前端 `security-events-panel.test.tsx` 12 项通过（原 10 项 + reveal 正向/隐藏 2 项），
  mock 补齐 `getSecurityEventSource`；`tsc -b` 通过。
- 构建（2026-10-14 19:5x）：`npm run build` 通过，入口 `/assets/index-eEhEE8Ne.js`，
  公共外壳 gzip 254538 B / 256000（余量 1462 B），公开图未混入 `pages/admin` 或终端/图表依赖。
- 后端门禁：`go build ./...`、`go vet`、`goimports-reviser -set-exit-status`（0 差异）、
  `golangci-lint run ./...`（0 issues）通过；`internal/store`（13.4s）与 `internal/adminauth`（9.9s）
  全绿；`internal/api/handler` 在跳过 7 项需要真实监听的用例（沙箱 `listen` 为 `EPERM`）后全绿，
  这 7 项与本次改动无关。
- 沙箱 `/tmp` 曾于本轮被构建缓存写满（tmpfs 6.3G，`no space left on device`），已清理过期的
  v12/v13/v14 候选目录，释放约 1.6G，后续构建与 lint 恢复正常。

### 2026-10-14 v17 批次统一补测（按用户“大任务完成后统一补测”的要求）

后端覆盖率（`go test -cover`，本沙箱可运行的包）：

| 包 | 补测前 | 补测后 |
| --- | --- | --- |
| `internal/loginsecurity` | 77.4% | 96.8% |
| `internal/adminauth` | 87.9% | 92.5% |
| `internal/maintenance` | 83.5% | 86.4% |
| `internal/store` | 85.2% | 86.0% |

新增/扩充用例：

- `internal/loginsecurity/summary_test.go`：来源摘要的“前缀优先 / 分类兜底 / 未知兜底”与设备摘要的已知片段拼接、未知片段剔除、全空兜底。
- `internal/adminauth/login_security_console_test.go`：控制台服务层未认证/禁用管理员拒绝、空 ID 与不存在事件、
  确认写入审计且重复确认报错、指标快照的拷贝语义与未启动服务分支、诊断计数与投递年龄、服务主体审计读取时
  清空会话前缀并继承 `WithAuditMetadata` 上下文。
- `internal/maintenance/security_test.go`：`WithSecurityStore` 注册后的保留清理（批次 5000、截止时间为当前时刻）
  与清理失败时维护状态落为 `failed`。
- `internal/store/security_paths_test.go`：失败登录准入的非法输入回滚（不留审计行）、阈值窗口首次锁定与
  “同窗口只下发一次”、`EmitThresholdEvent` 未标记窗口与非法事件回滚、指纹回看窗口计数、路由状态写入校验、
  事件筛选与分页边界。
- `internal/store/security_delivery_test.go`：通知投递证据（通道名、状态、尝试次数、错误码、更新时间）与四种
  投递状态（failed/pending/delivered/none）的列表呈现，以及筛选上下界归一化（默认 50、上限 200、负偏移归零）。
- 顺带修正：事件筛选此前不校验投递状态，词表外的值会直接进入 SQL 条件；现在与其它筛选一致返回
  `ErrSecurityFilterInvalid`（HTTP 400），不再退化为查询失败。

门禁：`goimports-reviser -set-exit-status`（0 差异）、`golangci-lint run ./...`（0 issues）、
`go vet`、`go build ./...` 通过；`internal/api/handler` 在跳过 7 项需要真实监听的用例后全绿。
前端 140 文件 / 704 用例全绿，`tsc -b`、`oxlint`（仅既有警告）、`npm run build` 通过。


### 2026-10-14 页面跳转复核（第三次反馈，含运行实例证据）

再次收到“前台后台点击链接后地址变化、内容停留在旧页面，需要手动刷新才更新”的反馈，本轮用可核查证据复核：

- 运行实例前端：`beat-server.service`（:8080）指向 `/opt/beat/current/static`，入口是单包
  `/assets/index-DE7vMPRD.js`（1.56 MB，`SHA256SUMS` 为 `22a05b41…`）；该包无 `lazy(`、无动态分片、
  无 `__vite__mapDeps`，即没有路由级代码分割，**不可能产生本现象**。:8081 预览自 2026-10-09 起未再更新静态
  目录，最后一次被浏览器拉取的入口是 `/assets/index-B31OmizM.js`（2026-10-09 08:29 导航修复之后的构建）。
- journal 证据（`journalctl`、本沙箱可读）：两个实例自 **2026-10-09 21:10** 之后没有任何浏览器文档或静态资源
  请求；2026-10-10 至今只有 Agent 上报（`127.0.0.1`/`::1` 的 `POST /api/v1/nodes/report` 与
  `GET /api/v1/network/assignments`）。也就是当前反馈对应的会话没有向这两台实例重新加载过页面。
- 结论：现象与 2026-10-09 08:29 之前的分片构建一致（旧标签页或已安装 PWA 窗口仍在运行会重建 `lazy` 身份
  的旧代码，点击链接时 transition 反复挂起，地址变化而内容不提交）。修复只对**加载了新入口分片**的页面生效。
- 现场自检：在目标页执行 `[...document.scripts].map(s=>s.src)` 或
  `performance.getEntriesByType('resource').map(r=>r.name).filter(n=>/\/assets\/index-.*\.js$/.test(n))`，
  与该实例 `index.html` 中的 `/assets/index-*.js` 比对；不一致即旧包，硬刷新一次即可（已安装 PWA 需在
  DevTools → Application 注销 Service Worker 并清空站点数据）。
- 现有前端已在代码层面覆盖该现象的全部已知失败路径，本轮未改动前后端逻辑：`route-chunk-cache` 稳定分片身份、
  `chunk-recovery` 在 `vite:preloadError` 上做一次受保护整页恢复（已在构建产物中确认该事件由 Vite
  `preload-helper` 无条件派发）、`chunk-stall` 10 秒挂起看门狗、`version-watch` 可见时与每 5 分钟比对入口分片。
  沙箱限制（写 `/opt` 只读、TCP `listen` EPERM、无法启动 Chromium）使得无法把工作树前端落到实例做实机复验，
  因此实例仍各自运行旧构建。

### 2026-10-14 页面跳转复核（第四次反馈，定位到具体会话 + 部署缺口）

再次收到“前后台点击链接不跳转、手动刷新才渲染”的反馈，这次把用户设备的会话逐条还原，结论比前几轮更硬：

- 手机侧（`2408:8459:3261:7ff5:43a:bfff:fe0a:eedd`，公网访问 :8081）两次加载：
  `08:11:01 GET /` → 拉取 `/assets/index-Ch-lloUw.js`（**修复前**入口）；
  `08:57:54 GET /` → 拉取 `/assets/index-B31OmizM.js`（**修复后**入口）。08:57:59–09:00:05 之间它连续
  拉取 `node-detail`、`admin`+`groups`、`settings`、`accelerators`、`lifecycle`、`operations`、`security`、
  `nodes`、`metrics`、`custom-metrics` 等分片，说明**新文档点击即跳转**，与 2026-10-09 的 Chromium 复验一致。
- 但同一次会话里，`08:58:52` 拉取 `lifecycle` 分片后，`08:59:36` 又出现整页请求 `GET /admin/lifecycle`
  ——即“点了链接、内容没提交、手动刷新”的残留路径在**已部署构建上仍会发生**。该构建没有 `chunk-stall`
  10 秒看门狗与 `chunk-recovery`（其构建产物分片清单里没有 `version-watch-*`/`chunk-stall-*`），
  所以这类残留只能靠用户手动刷新。
- 两台实例的最新浏览器活动：:8080 为 2026-10-07 11:29，:8081 为 2026-10-09 21:10（仅 auth 接口）；
  最近 3 天没有任何页面/静态资源请求（`journalctl --since "-3 days" | grep -E "assets/index-|GET / "` 为空）。
  也就是说实例上的旧文档不会被新代码改变，只有“重新加载文档”才会切到新构建。
- 结论：现象有两条来源——①长期驻留的旧文档（08:11 那份修复前入口）本身就会卡死；
  ②已部署的修复后构建缺少自愈防线，偶发卡住仍需手动刷新。**根治手段是把带自愈防线的构建发布上去**，
  而该构建目前只存在于工作树，两个实例都没有。
- 本轮改动（`webui/src/lib/version-watch.ts`）：版本巡检补上 `pageshow` 触发。从前进后退缓存恢复的
  文档不会重新执行入口分片，部分浏览器也不再派发 `visibilitychange`，此前只能等 5 分钟间隔；
  现在恢复事件到达即比对，发现新入口立刻整页加载一次。`version-watch.test.ts` 增补 bfcache 恢复用例
  （10 项通过），路由/分片/自愈 7 文件 41 项、全量 705 项通过，`tsc -b`、`oxlint`（仅既有 warning）、
  `npm run build` 通过，入口 `/assets/index-BPtqT6jR.js`。
- 仍需宿主执行（沙箱 `/opt` 只读、无 `sudo`/`systemctl`）：按匹配版本发布一次服务端（`scripts/beatctl.sh`
  的 `install|upgrade` 流程，含数据库迁移），让实例同时拿到自愈前端；发布脚本“保留旧 hashed 资源”属发布
  基础设施改动，需所有者确认后再实施。

### 2026-10-14 指标工作台面板级交互 + 公共外壳预算收口

按 `docs/beat/metric-workspace-contract.md`（“刷新失败保留上次良好数据并提供可见重试入口”）与
`docs/beat/metric-catalog-dashboard-design.md`（图标按钮需 tooltip、可放大面板查看）补齐三个面板级缺口：

- 面板级重试：`workspace-poller.ts` 新增 `retry(panelId)`，只重查单个面板；同一面板重复重试会中止在途请求，
  旧响应不再发布；`stop()` 会中止在途重试。`use-workspace-queries.ts` 暴露 `retryPanel`，
  失败态在面板头部下方渲染 `Retry`，未传 `retry` 时（如只读看板）不渲染按钮。
- 面板大图查看：面板头新增“放大查看”图标，`Dialog` 内以 `WorkspaceDataView height={360}` 复现该面板，
  只放大当前面板，不修改看板定义（`chart.tsx`/`data-view.tsx` 增加可选 `height`）。
- 图标按钮语义：`HeaderIcon`/`PanelMenu` 用 `aria-label` + 原生 `title`。shadcn `Tooltip` 组件会显著增加
  公共外壳 gzip，本轮预算不允许，这是刻意取舍；预算若放开应换回 `Tooltip/TooltipTrigger/TooltipContent`。

公共外壳预算收口（本轮最硬的约束）：

- 门禁在 `webui/scripts/public-shell-plugin.ts`，`gzipBytes > 250 * 1024` 即失败。为测量曾临时改成 `> 0`，
  已还原为 `250 * 1024`。
- 最终构建（含上述全部功能）公共图 gzip 合计 **254,643 B / 限 256,000**，余量 **1,357 B**（`node:zlib`
  口径约 255.4 KiB）。曾把 `PublicPWA` 改成动态 import 试图省预算：实测反而多 5 B，并把更新提示条移出
  Service Worker 预缓存集合，已回退为静态引入。
- 构建产物：入口 `/assets/index-DGuxZI56.js`，公共 allowlist 版本 `96a96796308ba829`（58 项）；
  自愈分片 `version-watch-*.js`/`chunk-stall-*.js` 是动态分片，不进预缓存集合（预缓存只覆盖静态公共图，
  网络导航仍由 `sw.js` 网络优先处理）。

顺带修复一处本轮的测试竞态：路由改为懒加载后，`app-navigation.test.tsx` 首屏断言
`screen.getByText(/Matching:/)` 会早于仪表盘分片解析，全量跑出现过一次失败；已改为 `findByText`，
单文件连跑 5 次稳定通过。

验证（本机沙箱内）：`tsc -b` 通过；`oxlint` 12 条 warning 全为既有项；`vitest run` 全量 **711 项通过**；
`npm run build` 在真实门禁下通过。仍待宿主执行：`/opt` 在沙箱内只读，无法把该构建发布到实例，
线上实例仍各自运行旧构建（见上一节“部署缺口”）。

### 2026-10-14 第五次复核：过渡挂起机制探针与待发布包

再次收到“前后台点击链接后不跳转、需手动刷新”的反馈，本轮用真实运行时探针把机制问清楚，并准备可直接发布的升级包：

- 探针（React 19 + react-router 8 + 真实 `LazyRoute`，对永不 resolve 的 loader 做原生点击）：
  路由过渡挂起时地址已变为 `/slow`，旧内容被卸载并显示 `RouteLoading`，结果
  `url=/slow loading=true homeStillVisible=false`。也就是说“点击后停在原地”在本工程里表现为加载态
  停留，而不是旧内容长期保留。因此 `chunk-stall` 的 10 秒看门狗覆盖这一类，分片失效由
  `chunk-recovery`（`vite:preloadError` 一次受保护整页恢复）覆盖，服务端换版由 `version-watch`
  在页面可见/`pageshow`/每 5 分钟比对入口分片覆盖。
- 运行实例仍停留在 2026-10-09 构建（:8081 `v14-mtsfix-20261009`，:8080 更早的单包构建），三层自愈
  都不在其中；已被浏览器载入内存的旧文档只能硬刷新一次，任何服务端改动都无法修复已经在运行的旧代码。
- 本工作树已生成预览升级包 `release/preview-v16-20261014/`：`beat-server`/`beat-agent`
  `0.2.0-dev+worktree.20261014`、入口 `/assets/index-JsucXJbh.js`、158 个文件 `sha256sum --check` 通过，
  `DEPLOY.md` 给出停服、冷备份、切换、schema v14 → v16 迁移校验与回滚步骤。
- 本轮沙箱不可写 `/opt`（`touch /opt/beat/...` 返回 read-only file system），无 systemd 总线、无
  capability（`CapEff=0`），发布必须在宿主机执行；发布后浏览器硬刷新一次即收敛到新版。

定向回归（本轮实跑）：`route-chunk`、`route-navigation`、`app-navigation`、`chunk-recovery`、
`chunk-stall`、`version-watch` 共 6 文件 36 项通过；`npm run build` 通过（公共外壳 250 KiB 门禁）。

### 2026-10-14 第六次复核（访问日志证明：两个实例今天没有下发过任何页面）

再次收到“前台后台点击链接后不跳转、需手动刷新”的反馈，本轮直接用访问日志判定反馈来源：

- 当前 `/var/log/messages`（2026-10-11 00:24 轮转后）里两个实例的 `client_ip` 只有 `::1` 与
  `127.0.0.1`。2026-10-14 全天 `beat-server[1872224]`（:8081）与 `beat-server[1796472]`（:8080）
  只收到 `/api/v1/nodes/report`（32,275 条）与 `/api/v1/network/assignments`（5,386 条）两类 Agent
  请求，`GET /` 与 `/assets/*` 均为 0 条。静态请求本身是有日志的（轮转文件里 `/assets/` 有 860 条），
  所以不是“静态请求不记日志”，而是确实没有浏览器请求到达。
- 最后一次外部浏览器页面加载仍是 2026-10-09 10:48:33（下发 `/assets/index-B31OmizM.js`，含导航修复）。
  本轮反馈因此仍来自浏览器内存里的旧文档：修复前入口 `index-Ch-lloUw.js` 在过渡重试时重建 lazy 身份，
  点击既不提交新内容也不产生任何请求，只有硬刷新能恢复。已在运行的旧文档无法由服务端修复。
- 处理顺序：先硬刷新一次（Ctrl+Shift+R）确认收敛；独立窗口/PWA 则关闭重开，必要时在 DevTools
  注销 Service Worker 并清站点数据。之后再由宿主机把工作树前端发布到 :8081/:8080，`version-watch`
  才会接管“换版后自动整页加载”。
- 升级包一致性修正：`release/preview-v16-20261014` 在 22:15 重新构建后入口已变为
  `/assets/index-Cb5khZCE.js`，`DEPLOY.md` 的验收 grep 由过期的 `/assets/index-JsucXJbh.js` 更正，
  `SHA256SUMS` 同步更新后 `sha256sum --check --status` 通过（158 项）。
- 22:44 补充：后台接入页补齐可选配置段后重新构建，包内前端已刷新为入口
  `/assets/index-C3loDFcy.js`，`DEPLOY.md` 与 `SHA256SUMS` 再同步（305 项校验通过）；包内二进制未变。

### 2026-10-14 v25 看板协作面与预览包刷新

按批次 `dashboard-collaboration`（v25）补齐指标看板协作能力，代码已落地工作树，尚未部署到实例：

- 迁移：`currentSchemaVersion` 17 → 25；v18–v24 注册为 `reserved_*` 空操作（尚未发布，实现各批次时必须把真实
  DDL 写进同一条目），v25 为 `dashboard_collaboration`，新增 `dashboard_annotations`、
  `dashboard_snapshots`、`dashboard_panel_library`、`dashboard_panel_alerts` 四张表（含索引、CHECK 与长度上限）。
- 后端：`internal/store/dashboard_{annotation,snapshot,panel_library,panel_alert}*.go` 提供注释可见性/上限、
  快照 token 分享与撤销、面板库自持、面板级告警聚合；`internal/api/routes_metric_dashboards.go` 暴露公开
  注释/快照读取与一组 `/api/v1/admin/metrics/*` 管理路由；对应模型校验在 `internal/model/dashboard_collaboration.go`。
- 前端：指标工作台新增注释、快照、面板库、面板告警徽标；公开快照复用公共兜底分片
  （`/metrics/snapshots/{token}` 走 `PublicFallback`），避免公共外壳预算被新路由挤爆。
- 沙箱内验证：`go test ./internal/store/ ./internal/model/` 通过；前端 `vitest run` 147 文件 / 741 用例、
  行覆盖 93.1%，`tsc -b`、`oxlint`（仅既有 warning）、`npm run build`（公共外壳 250 KiB 门禁）均通过。
  `internal/api` 的 socket 用例在沙箱内必然 panic（`listen` EPERM），需宿主机复跑。
- `release/preview-v16-20261014` 已用 v25 代码重建：`beat-server`/`beat-agent`
  `0.2.0-dev+worktree.20261014`（schema v25），前端入口 `/assets/index-BYd1K40A.js`，
  `DEPLOY.md` 验收 grep 与 `SHA256SUMS`（341 项）同步，`sha256sum --check --status` 通过。
- 部署仍需宿主机：沙箱 `/opt` 只读、无 systemd 总线，`DEPLOY.md` 给出停服、冷备份、切换、
  schema v14 → v25 迁移校验与回滚步骤。主题构建脚本接入门禁仍未获批准，本轮未改动。

### 2026-10-14 注释时间轴叠加（v25 补全）

- 指标工作台把看板注释按窗口叠加到面板时间轴上（裁剪色带 + 起点线），面板级注释只出现在所属面板；
  注释标签列表新增所属面板徽标，创建对话框可切换「整个看板 / 单个面板」作用范围。
- 预览包前端刷新为入口 `/assets/index-CpB7ugRO.js`，`DEPLOY.md` 与 `SHA256SUMS`（372 项）同步，
  `sha256sum --check --status` 通过；包内二进制仍是 v25 构建。
- 验证：新增 10 项定向用例，全套 `vitest` 通过（语句 90.2%、行 93.13%），`tsc -b`、`oxlint`、
  `npm run build`（250 KiB 公共外壳门禁）通过。

### 2026-10-15 自动发现式入网（v26）升级包

- 迁移：`currentSchemaVersion` 25 → 26；v26 为 `agent_discovery`，新增 `agent_enrollment_grants`、
  `agent_enrollment_challenges`、`agent_enrollment_requests` 三张表，并为 `nodes` 增加
  `credential_state`（legacy/active/pending_claim/revoked）回填。
- 接入链路：管理端 `/admin/enrollment` 创建或轮换授权后，弹窗直接给出可执行的
  `beat-agent enroll --server <地址> --grant-stdin <<'BEAT_ENROLL' …` 命令；目标机 Agent 协商配置、
  写 `agent.json`、安装 systemd 单元并等待审批，重复执行续接同一请求而不新建节点。
- 沙箱内验证：`go build ./...`、`go vet ./internal/... ./cmd/...`、`goimports-reviser -set-exit-status`、
  `golangci-lint run ./internal/... ./cmd/...`（0 issues）通过；`internal/agentenrollment`、`cmd/agent`
  与 handler 入网用例通过；前端 `tsc -b`、`vitest run` 193 文件 / 1054 用例、行覆盖 91.43%、
  `oxlint src` 0 error、`npm run build` 通过。
- `release/preview-v26-20261015` 为可部署包：`beat-server`/`beat-agent`
  `0.2.0-dev+worktree.20261015`，前端入口 `/assets/index-CeAAiDrE.js`，附
  `deploy-preview-8081.sh`（一条命令停服、冷备份、切换、启动、校验，失败自动切回）与 `SHA256SUMS`
  （619 项，`sha256sum --check --status` 通过）。
- 部署仍需宿主机：本沙箱无 capability、`/opt` 只读、无 systemd 总线且禁止创建 socket，实测
  `listen tcp 127.0.0.1:8081: socket: operation not permitted`。宿主机执行
  `release/preview-v26-20261015/deploy-preview-8081.sh` 即完成 8081 实例升级，回滚用同一脚本的
  `rollback <备份目录>`；v26 数据库不能被旧二进制打开，回滚必须连同状态归档一起恢复。

### 2026-10-15 面板级结果变换（v27 升级包）

- 内容：看板面板的 `transformations` 链（`rename_by_regex` / `filter_by_value` / `sort_by` / `limit`，
  最多 4 条、按序执行），服务端逐条校验种类与字段，前端编辑器与 JSON 导入共用同一套错误键；
  数据库版本不变，仍是未发布的 v26 `agent_discovery`。
- 沙箱内验证：`go build ./...`、`goimports-reviser -set-exit-status ./...`、
  `golangci-lint run ./internal/... ./cmd/...`（0 issues）通过；`vitest run` 194 文件 / 1071 用例通过；
  `tsc -b` 干净；`npm run build` 通过，公共外壳 gzip 237,650 B + 11,908 B = 249,558 B / 256,000 B，
  管理端私有分片 612.44 kB（598 KiB）/ 600 KiB 预算内。
- 迁移预演：以宿主机冷备份 `pre-v14-mtsfix-20261009` 的状态归档（schema 14，与线上同源）复刻到 `/tmp`，
  用本包 `beat-server` 打开后 schema 14 → 26、`integrity_check` = ok、`foreign_key_check` 无违规，
  管理员与节点凭据保留（`credential_state` = `active`），五张 `agent_enrollment_*` 表就绪。
- `release/preview-v27-20261015` 为可部署包：`beat-server`/`beat-agent`
  `0.2.0-dev+worktree.20261015`，前端入口 `/assets/index-2G9aUopX.js`，附
  `deploy-preview-8081.sh`（一条命令停服、冷备份、切换、启动、校验，失败自动切回）与 `SHA256SUMS`
  （182 项，`sha256sum --check --status` 通过）。
- 部署仍需宿主机：本沙箱无 capability、`/opt` 只读、无 systemd 总线且禁止创建 socket，实测
  `listen tcp 127.0.0.1:8081: socket: operation not permitted`；systemd 切换必须由宿主机执行，
  命令见包内 `DEPLOY.md`，回滚用同一脚本的 `rollback <备份目录>`。

### 2026-10-15 指标探索页、阈值显示模式与分片修复（v29 升级包）

- 内容：指标探索页 `/admin/metrics/explore`（对标 Grafana Explore，查询与范围编码进 URL）；
  阈值显示模式 `threshold_mode`（`off`/`line`/`area`/`background`，空值等于 `line`，因此既有看板不变）；
  以及管理端文案目录分块（`advancedChunks`），把被新文案顶破的 600 KiB 私有分片门禁修回预算内。
- 沙箱内验证：`tsc -b` 干净，`oxlint src` 0 error（16 条既有 warning），`vitest run` 195 文件 /
  1074 用例通过；`go build ./cmd/... ./internal/...`、`gofmt -l internal/model`、
  `goimports-reviser -set-exit-status ./...`、`golangci-lint run ./internal/model/...`（0 issues）、
  `go test ./internal/model/` 均通过；`npm run build` 通过两档分片预算
  （公共图 gzip 238,011 B + 最大语言分片 11,908 B = 249,919 B / 256,000 B；管理端目录分块
  600.45 kB = 586 KiB / 600 KiB）。
- 迁移预演：以宿主机冷备份 `pre-v14-mtsfix-20261009` 的状态归档（schema 14，与线上同源，含 1 个节点）
  复刻到 `/tmp`，用本包 `beat-server` 打开后 schema 14 → 26、`integrity_check` = ok、
  `foreign_key_check` 无违规、`agent_enrollment_*` 五张表就绪、节点 `credential_state` = `active`。
  预演副本里的 MTS 存储会报 `open manifest part failed`，原因是 MTS 分片清单内嵌了
  `/var/lib/beat-preview/data/beat_mts/...` 绝对路径，副本换目录后无法定位；线上升级不改数据目录，不受影响。
- `release/preview-v29-20261015` 为可部署包：`beat-server`/`beat-agent`
  `0.2.0-dev+worktree.20261015`（commit `14fae14+dirty`），前端入口 `/assets/index-BuImmmt1.js`，附
  `deploy-preview-8081.sh`（一条命令停服、冷备份、切换、启动、校验，失败自动切回）与 `SHA256SUMS`
  （190 项，`sha256sum --check --status` 通过）。包内 `DEPLOY.md` 列出验收路径。
- 部署仍需宿主机：本沙箱无 capability、`/opt` 与 `/var/backups` 只读、无 systemd 总线且禁止创建 socket。
  实测执行本包脚本在第一个写操作处停下：`install: cannot create directory
  '/var/backups/beat-preview/pre-v29-...': Read-only file system`（此前 `systemctl is-active` 已因总线不可达
  失败，因此没有停服、没有改动线上）。宿主机执行
  `release/preview-v29-20261015/deploy-preview-8081.sh` 即完成 8081 实例升级；回滚用同一脚本的
  `rollback <备份目录>`，v26 数据库不能被 v14 二进制打开，回滚必须连同状态归档一起恢复。

## 2026-10-17 指标即时查询与 All 变量（v60–v64 部署）

- v60：通知队列在关闭期间不再把 SQLite 取消回滚上报为存储故障；v61/v62：看板多值变量新增
  Grafana 风格 All 选项（`include_all` 只能与 `multi` 同用，查询与重复面板按当前授权节点/分组展开）；
- v63/v64：C06 新增即时查询 `POST /api/v1/metrics/query/latest`（公开与管理员各一条路由），
  一次最多 200 条序列，返回每条序列当前最新**原始**样本；工作台检查器「数据」页新增「最新样本」一行，
  最新样本按应用语言显示日期与时分秒，即时查询失败只降级为提示；
- 实机证据：`/admin/metrics` → 面板「Inspect data」显示
  `Latest samples · raw newest sample CPU usage 8.49% · 10/17/2026, 03:17:55`，
  `POST /api/v1/admin/metrics/query/latest` 返回 200 且 `state=ok`；
  All 变量经新建看板→详情→允许多选→包含 All→保存→对话框选 All，摘要显示 `All` 且服务端持久化 `include_all: true`；
- 部署：`0.2.0-dev+worktree.20261017.9`（v63，入口 `assets/index-CByNwIPJ.js`）与
  `...20261017.10`（v64，入口 `assets/index-D9yuuJtB.js`），冷备份
  `/var/backups/beat-preview/pre-v63-20261016T191418Z`、`pre-v64-20261016T191735Z`；schema 仍 v27。

## 2026-10-17 指标探索即时查询（v65 部署）

- `/admin/metrics/explore` 新增 Grafana 的查询类型 `Range | Instant`；Instant 走 v63 的
  `POST /api/v1/admin/metrics/query/latest`，按「节点 + 指标」去重成批取最新原始样本；
- 即时模式隐藏最大点数选择、显示说明，结果渲染成「即时结果」表格（序列 / 最新值 / 采样时间）；
  带加速器设备的行会提示并禁用执行；查询类型写入 URL（`type=instant`）；
- 实机（Playwright）：切到 Instant 后执行只触发一次即时请求，区间请求数不变，
  表格显示 `6.21% · 10/17/2026, 03:27:52`，零失败请求；
- 门禁：`vitest run` 215 文件 / 1205 例通过、`tsc -b` 0 错误、行覆盖 90.82%；
  部署 `0.2.0-dev+worktree.20261017.11`，冷备份 `/var/backups/beat-preview/pre-v65-20261016T192723Z`；schema 仍 v27。
