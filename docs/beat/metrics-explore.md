# 指标探索（Grafana Explore 等价物）

验证日期：2026-10-17。本批不新增 SQLite 迁移、不改备份格式，也不改变既有看板行为。

## 为什么做

看板适合「已经知道要看什么」的场景；排障时需要的是不做落库的临时查询、随时换指标和节点、
再把当前视图交给同事。Grafana 用 Explore 承担这一段，Beat 此前只能先建看板再删掉。

## 形态

- 路由：`/admin/metrics/explore`（管理员会话内）。入口在指标工作台页头的「指标探索」按钮。
- 查询类型：`Range`（默认）与 `Instant`。Range 走区间分桶；Instant 调用
  `POST /api/v1/admin/metrics/query/latest`，按「节点 + 指标」去重后一次取回每条序列最新的原始样本，
  此时时间范围与最大点数都不参与查询（页面隐藏最大点数选择并给出说明），结果渲染成
  「即时结果」表格（序列 / 最新值 / 采样时间，空样本显示“暂无样本”而不是 0）；
  Instant 只支持节点级指标，任何可见行带加速器设备时逐行提示并禁用执行，不静默丢维度。
  查询类型写入 URL（`type=instant`），复制链接与复制 JSON 都按当前类型输出。
- 查询：最多 4 行（`A`–`D`），每行独立选择指标、聚合方式、节点、加速器设备（节点有设备时用下拉，
  否则填设备 ID）与颜色，可单行隐藏。聚合取值沿用看板契约（avg/min/max/sum/last/rate），
  切换指标时回落到该指标的默认聚合。
- 范围：复用工作区的时间控件（5 分钟到 30 天预设、自定义绝对范围、刷新间隔、暂停、手动刷新），
  另有最大数据点数（60/300/600，服务端上限 600）。
- 结果：图与表复用看板同一套渲染（`WorkspaceDataView` / `WorkspaceDataTable`），因此单位、阈值取色、
  图例、缩放与看板完全一致；表视图与「复制查询 JSON」等价于 Grafana 的查询检查器。
- 分享：查询与范围整体编码进 URL（`?q=…&range=…` 或 `?from=…&to=…&points=…`），
  「复制链接」即可让同事打开同一视图。URL 解析按白名单校验：未知指标、未知节点、非法聚合与非法颜色
  的行会被丢弃，全部不可用时回落为默认查询，绝不把非法载荷发给服务端。
- 自动刷新只在相对范围下开启；绝对范围是固定窗口，不重复查询。标签页隐藏时不发请求，回到前台补一次。

## 边界

- 探索结果不落库：没有「保存为看板」动作，需要沉淀时按看板流程新建面板（本批未做一键转面板）。
- 查询走 `POST /api/v1/admin/metrics/query`（Range）或 `POST /api/v1/admin/metrics/query/latest`
  （Instant），权限与看板完全一致，复用同一条准入、可见性与目录启用校验。
- 文案目录 `explore.*` 为按需分片（`webui/src/context/metric-explore-translations.ts` 及
  zh-TW/ja/id 分片），随工作区分片注册，不进入公共外壳预算。

## 验证（沙箱内实跑，2026-10-15）

- `tsc -b` 干净；`oxlint src` 0 error（16 条既有 warning，无新增）；
- `vitest run src/pages/admin/metrics-explore.test.tsx` 3 例通过：加载查询行并执行查询后渲染图与表、
  「复制链接」把查询写进 URL、删除最后一行后仍能通过提示与「添加查询」恢复；
- `vitest run src/context/catalog-integrity.test.ts src/context/locales.test.ts src/context/pseudo-locale.test.ts
  src/pages/admin-shell.test.tsx` 30 例通过，五语言键集完整。

未覆盖：宿主机浏览器上的真实交互与可访问性演练、`npm run build` 分片预算复核（由批次收尾统一执行）。

## 即时查询实机验证（2026-10-17，v65）

- 前端：`vitest run` 215 文件 / 1205 例通过、`tsc -b` 0 错误、`oxlint src` 仅既有 warning、
  `test:coverage` 行覆盖 90.82%；公共图 240,316 B + 最大语言分片 13,711 B = 254,027 B / 256,000 B。
- 新增用例：`src/lib/metric-explore.test.ts` 覆盖即时载荷去重、设备限制与 URL 往返；
  `metrics-explore.test.tsx` 覆盖「切到 Instant → 执行 → 只发即时查询并渲染最新值」。
- 浏览器（Playwright，8081 v65）：`/admin/metrics/explore` 先执行一次 Range（2 次区间请求），
  切到 Instant 后最大点数选择消失、说明文案出现，执行只触发一次
  `/api/v1/admin/metrics/query/latest`，区间请求数不变；「即时结果」表格显示
  `A CPU usage · 当前主机 · 实时采集 6.21% 10/17/2026, 03:27:52`，URL 含 `type=instant`，
  全程零失败请求。
