# Beat 主题制作指南

本文对应当前实现：包清单 schema **1**、声明式定义 schema **1**、脚本通信协议 **2**。
三个版本分别管理文件清单、原生布局、iframe 通信，不可互换。

## 1. 选择运行方式

| 方式 | 适合的用途 | 可控制的内容 |
| --- | --- | --- |
| `declarative` | 制作配色、节点列表和详情布局，推荐从这里开始 | Beat 原生组件的布局、密度、圆角、明暗调色板 |
| `sandboxed` | 完全自定义的公开展示页 | 隔离 iframe 中的 HTML/CSS/JS，通过 SDK 接收公开快照 |

主题应用于公开首页和节点详情。后台管理、登录和账户安全页面始终使用 Beat 自身界面。
声明式主题无需 npm 构建，也不加载包中的 CSS/JS；它只解释下述 `theme.json`。

## 2. 从可用示例开始

| 名称 | 源码目录 | 首页 / 详情 | 适合的场景 | 关键字段写法 |
| --- | --- | --- | --- | --- |
| Slate | `themes/slate` | 分组卡片 / 均衡 | 默认基线：多数站点的通用首页 | `radius: 6`，其余留空取默认值 |
| Operations | `themes/operations` | 节点表格 / 紧凑 | 值班大屏与密集对比 | `overview: "table"`、`density: "compact"`、`radius: 4` |
| Canvas | `themes/canvas` | 宽幅卡片 / 焦点 | 突出单机 CPU 与首张趋势图 | `resources: ["cpu", "memory"]`、`show_remark: false` |
| Graphite | `themes/graphite` | 节点表格 / 均衡 | 长时间值守，直角扁平表面 | `radius: 0`、`show_host: false` |
| Aurora | `themes/aurora` | 分组卡片 / 焦点 | 品牌化展示，留白与圆角更明显 | `overview: "cards"`、`radius: 8` |
| Terminal | `themes/terminal` | 节点表格 / 紧凑 | 终端风格值守，等宽字体 | `font: "mono"`、`radius: 2` |
| Paper | `themes/paper` | 分组卡片 / 均衡 | 暖白衬线阅读感，隐藏速率行 | `font: "serif"`、`show_network: false` |
| Midnight | `themes/midnight` | 宽幅大格 / 焦点 | 深色机房环境，圆角大格 | `radius: 6`，并显式写出全部 `show_*` 开关 |
| Solar | `themes/solar` | 节点列表 / 均衡 | 手机与告警屏，暖色高对比清单 | `density: "compact"`、`show_remark: false` |
| Pulse | `themes/pulse` | 脚本状态板（iframe 沙箱） | 完全自定义渲染的展示页 | `runtime: "sandboxed"` + `dist/index.html` 入口 |

前九套是声明式示例，均包含浅色和深色调色板与 PNG 预览图，以及完整可编辑源码。
`Slate`、`Paper`、`Terminal` 额外声明了 `configuration`（第 4.1 节）：`Slate` 收窄首页布局并放出标签开关，
`Paper` 让站点在衬线/系统字体间切换并决定是否显示速率行，`Terminal` 在等宽/系统字体间切换并放开密度。
`Pulse` 是脚本主题示例（`runtime: "sandboxed"`），带 `dist/` 入口、CSS、JS 与 SDK 副本，
只运行在隔离 iframe 中。后台“站点设置 → 已安装主题”下方可直接安装示例或下载 ZIP。
分发文件位于 `webui/public/themes/{slate,operations,canvas,graphite,aurora,terminal,paper,midnight,solar,pulse}.theme.zip`。

源码准备和打包（在仓库根目录执行，需项目要求的 Go 版本）。推荐直接生成骨架，它会一次性写好
`beat-theme.json`、`theme.json`、`license.json` 和一张与调色板同色的占位预览图，并在写出前跑一遍
生产校验器：

```bash
umask 077
go run ./cmd/theme init --name "My fleet" --output my-theme --overview list --density compact --accent teal
# 按需替换 my-theme/preview.png 与 license.json，再打包校验。
go run ./cmd/theme pack --source my-theme --output my-theme.theme.zip
go run ./cmd/theme validate --archive my-theme.theme.zip
```

`init` 的选项：`--name`（必填，也是默认 id 的来源）、`--output`（必填）、`--id`（默认
`beat-<名称短横线化>`）、`--overview`（`cards`/`table`/`tiles`/`list`）、`--detail`
（`balanced`/`compact`/`spotlight`）、`--density`（`comfortable`/`compact`）、`--font`
（`system`/`serif`/`mono`）、`--accent`（`blue`/`teal`/`amber`/`violet`）、`--radius`（0–8）、
`--force`（写入非空目录）。四个强调色调都使用经过对比度校验的调色板；改色后请重新执行
`pack`，让校验器确认对比度仍然达标。也可以照旧手写 JSON 或复制 `themes/slate` 等示例目录。

发布归档里带有编译好的 `beat-theme`（无需 Go 工具链），也可以先 `go build -o beat-theme ./cmd/theme`，之后直接使用 `beat-theme pack` 和
`beat-theme validate`。工具与服务端共用生产校验器，不是另一套近似规则。
成功输出主题 ID、版本、运行方式、文件数和整个 ZIP 的 SHA-256；失败返回非零退出码。

`pack` 自动计算文件大小、SHA-256 和排序后的完整清单，以固定顺序生成 ZIP。
相同输入会得到相同 ZIP。已有 `files` 内容会被重新计算；源码清单不会被改写。
输出可以放在源码目录内，该输出文件会被排除，但不得覆盖 `beat-theme.json`。
不要在主题源码目录内保留 `.git`、`node_modules`、测试、临时 ZIP 或 README；
除了指定输出，工具会检查全部文件，而不会默默忽略它们。

## 3. 目录与包清单

```text
my-theme/
  beat-theme.json
  theme.json          # declarative 必需
  preview.png         # 实际页面截图
  license.json        # 示例附带的版权与许可说明
```

脚本主题用入口目录代替 `theme.json`（详见第 6 节）：

```text
my-script-theme/
  beat-theme.json
  preview.png
  license.json
  dist/
    index.html        # 唯一入口，必须引用包内 CSS/JS
    theme.css
    theme.js
    beat-sdk.js       # 复制自 themes/sdk/bridge.js
```

用于 `pack` 的最小 `beat-theme.json`：

```json
{
  "schema_version": 1,
  "id": "my-status-theme",
  "name": "My status theme",
  "description": "Public fleet overview",
  "version": "1.0.0",
  "author": "Your name",
  "license": "MIT",
  "runtime": "declarative",
  "minimum_beat_version": "0.1.0",
  "preview": "preview.png",
  "files": []
}
```

这里的空 `files` 仅供源码打包使用；直接上传的 ZIP 必须带完整文件清单。

| 字段 | 说明 |
| --- | --- |
| `schema_version` | 固定为 `1` |
| `id` | 3–64 个 ASCII 字符，只允许小写字母、数字、`-`、`_` |
| `name`, `description`, `author`, `license` | 必填非空文本；许可由作者负责选择和附带 |
| `version` | 推荐发布版本 `主版本.次版本.修订版本`；修改包后递增版本 |
| `runtime` | `declarative` 或 `sandboxed` |
| `minimum_beat_version` | 支持的最小 Beat 版本，含边界 |
| `maximum_beat_version` | 可选的最大 Beat 版本，含边界；按三个数字版本段比较 |
| `preview` | 包内 PNG/JPEG/WebP 路径，必须出现在文件清单中 |
| `homepage`, `release_notes` | 可选主页和发布说明文本 |
| `supported_locales` | 可选语言标识列表；不会自动翻译作者自己的文本 |
| `configuration` | 可选；声明站点所有者可以调整哪些显示字段，见第 4.1 节 |
| `files` | 除清单自身外，每个文件的 `{path,size,sha256}`；大小为字节，哈希为小写十六进制 SHA-256 |

未知清单字段、遗漏/重复清单项、文件大小或哈希不匹配都会拒绝安装。
主题 ID + 版本以及归档摘要用于检测重复包；新发布应使用新版本，不覆盖已经安装的版本。

最小可安装包一共四个文件，`beat-theme.json` 的 `files` 由 `pack` 自动填满
（下面是打包后的形态，源码里写 `[]` 即可）：

```text
my-status-theme/
  beat-theme.json
  theme.json
  license.json
  preview.png
```

一个完整的声明式主题包（`beat-theme.json`）：

```json
{
  "schema_version": 1,
  "id": "my-status-theme",
  "name": "My status theme",
  "description": "Public fleet overview",
  "version": "1.0.0",
  "author": "Your name",
  "license": "MIT",
  "runtime": "declarative",
  "minimum_beat_version": "0.1.0",
  "preview": "preview.png",
  "supported_locales": ["en", "zh-CN"],
  "files": [
    { "path": "license.json", "size": 1080, "sha256": "…" },
    { "path": "preview.png", "size": 48213, "sha256": "…" },
    { "path": "theme.json", "size": 1420, "sha256": "…" }
  ]
}
```

### 3.1 `license.json`：随包许可说明

`license.json` 是主题包自带的许可与版权说明。它随 ZIP 一起分发、原样导出，
让下载者不必打开源码就能看到授权条款；`beat-theme.json` 的 `license` 字段则是安装、
列表和市场展示读取的正式字段。两者请保持一致。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `license` | 是 | 许可名称或 SPDX 标识符（如 `MIT`、`Apache-2.0`），与 `beat-theme.json` 的 `license` 对应 |
| `copyright` | 是 | 一行版权声明，通常是 `Copyright (c) <年份> <作者>` |
| `text` | 是 | 完整许可正文，UTF-8 文本，换行用 `\n` 转义 |

```json
{
  "license": "MIT",
  "copyright": "Copyright (c) 2026 Your name",
  "text": "MIT License\n\nCopyright (c) 2026 Your name\n\nPermission is hereby granted, free of charge, …"
}
```

行为与边界：

- 服务端**不解析** `license.json`，也不会因为缺少它而拒绝安装——安装校验只读
  `beat-theme.json` 的 `license` 字段。它是给人和下游分发用的说明文件，请自行保证准确。
- `go run ./cmd/theme init …` 会生成一份 MIT 模板，替换成自己选择的许可证即可；
  归档中的示例主题都带 `license.json`，可直接复制改写。
- `license.json` 必须是包内普通文件，和其它文本文件受同样的类型与大小限制
  （单个文本文件 ≤ 8 MiB、UTF-8），会进入 `files` 清单并参与整包摘要。
- 使用第三方字体、图标或代码时，请把它们的许可一并写进 `text`，并在
  `copyright`/`release_notes` 里注明来源；`LICENSE` 里描述的是 Beat 自身与主题 SDK 的授权，
  不能替代你的主题许可。

## 4. 声明式布局和配色

`theme.json` 是严格的 JSON 对象，不超过 64 KiB。完整示例见任意示例源码目录。

| 字段 | 允许值和行为 |
| --- | --- |
| `schema_version` | `1` |
| `overview` | `cards` 分组卡片；`table` 节点表格；`tiles` 宽幅卡片；`list` 紧凑状态列表（一行一个节点，含状态圆点、内联资源读数与可选网络值，适合手机与告警大屏） |
| `detail` | `balanced` 上方概览、下方图表；`compact` 宽屏左右分栏；`spotlight` 强调首项资源和第一张图表 |
| `density` | `comfortable` 或 `compact`；调整首页间距、卡片内边距、表格行高 |
| `radius` | `0`–`8` 的整数，基础圆角单位为 px，组件按比例使用 |
| `font` | 可选。`system`、`serif` 或 `mono`，只内置这三套字体栈；省略等于产品默认字体，不接受自定义字体名、`url()` 或外部字体文件 |
| `resources` | 可选。公开列表展示哪些资源行以及顺序，取值 `cpu`、`memory`、`disk`，1–3 项且不重复；省略等于按该顺序展示全部三项 |
| `show_network` | 可选。`false` 隐藏卡片底部的上下行速率，以及表格的“网络”列（`tiles` 布局本来不含速率行） |
| `show_host` | 可选。`false` 隐藏节点名下方的地址行；是否提供地址仍由站点设置的公开地址模式决定 |
| `show_tags` | 可选。`false` 隐藏 `tiles` 布局节点卡片上的标签 |
| `show_remark` | 可选。`false` 隐藏 `tiles` 布局节点卡片上的公开备注 |
| `light`, `dark` | 必须分别提供完整的 24 个调色板字段，见下表 |

`resources`、`show_*` 是作者声明的内容字段：省略即沿用当前默认（全部资源、全部显示），
显式写 `false` 才隐藏。它们只作用于公开首页的节点列表（`cards`/`table`/`tiles`/`list`），节点详情页
仍由 `detail` 与站点设置决定；站点覆盖配置也刻意不包含这些字段，修改内容需要重新打包发布新版本。

```json
{ "resources": ["cpu", "memory"], "show_host": false, "show_remark": false }
```

示例 `themes/canvas` 用 `"resources": ["cpu", "memory"]` 与 `"show_remark": false` 演示磁贴内容收敛，
`themes/graphite` 用 `"resources": ["cpu", "memory"]` 与 `"show_host": false` 演示密集表格，
`themes/paper` 用 `"resources": ["cpu", "memory"]` 与 `"show_network": false` 演示收敛后的卡片。
`themes/terminal` 用 `"font": "mono"` 配合紧凑表格演示值守大屏。

`font` 同样是作者声明的字段，只接受 `system`、`serif`、`mono` 三个值，省略即沿用产品默认字体。
校验器拒绝自定义字体名、`url()` 与外部字体引用；公开页与预览只在主题根节点上设置字体族，
既不加载新字体，也不修改后台界面。脚本主题在自己的 iframe 内自行声明字体，不受此字段约束。

每个颜色必须是 `#RRGGBB`。不接受 `url()`、CSS 表达式、透明度、任意 CSS 类名或额外字段。
颜色只写入当前公开页面或预览根节点的 CSS 变量，不修改后台或整个文档的全局变量。
明暗模式跟随访问者当前选择；后台预览可单独切换，不影响访客。

| 颜色字段 | 用途 |
| --- | --- |
| `background` / `foreground` | 页面背景和正文 |
| `card` / `card-foreground` | 卡片背景和文字 |
| `popover` / `popover-foreground` | 浮层背景和文字 |
| `primary` / `primary-foreground` | 主操作、在线状态及其文字 |
| `secondary` / `secondary-foreground` | 次级表面、离线状态及其文字 |
| `muted` / `muted-foreground` | 弱化表面及辅助文字 |
| `accent` / `accent-foreground` | 强调或悬停表面及其文字 |
| `destructive` / `destructive-foreground` | 错误表面及文字 |
| `border` | 分隔线和边框 |
| `input` | 输入组件边框 |
| `ring` | 键盘焦点轮廓 |
| `chart-1` 到 `chart-5` | 图表系列颜色 |

生产校验器按 sRGB 相对亮度检查：上述八组背景/前景对比度至少 **4.5:1**；
`ring` 和五个图表颜色相对于 `card` 至少 **3:1**。两套调色板都必须通过。
还应人工检查手机布局、长节点名称、空数据、离线节点和键盘焦点，不能只看色值校验。

后台可覆盖四个显示字段（`resources` 与 `show_*` 属于作者声明，不在覆盖范围内），示例：

```json
{"overview":"table","detail":"compact","density":"compact","radius":4}
{"overview":"list","density":"compact"}
```

这些设置保存到现有主题记录的配置字段。省略的字段沿用包定义；“重置配置”保存 `{}`，
恢复包内默认值。配置最多 16 KiB，不支持插入 HTML、CSS、JS 或更改调色板；
配色与内容改动应发布新版包。配置只适用于声明式主题，脚本主题通过自己的代码定义展示方式。

### 4.1 `configuration`：把哪些字段交给站点所有者

站点覆盖默认只有 `overview`、`detail`、`density`、`radius` 四个显示字段。作者可以在清单里多声明
几个字段，让站点所有者自己决定字体、内容开关或更少的布局选项——改的都是渲染器内置的枚举，
主题代码、配色和结构不受影响。

```json
{
  "schema_version": 1,
  "id": "my-status-theme",
  "runtime": "declarative",
  "configuration": {
    "type": "managed",
    "items": [
      { "key": "overview", "name": "Overview layout", "options": ["cards", "table"] },
      { "key": "font" },
      { "key": "show_host" },
      { "key": "radius", "name": "theme.radius" }
    ]
  }
}
```

| 键 | 控件 | 可选取值 |
| --- | --- | --- |
| `overview` | 枚举 | `cards` / `table` / `tiles` / `list` |
| `detail` | 枚举 | `balanced` / `compact` / `spotlight` |
| `density` | 枚举 | `comfortable` / `compact` |
| `font` | 枚举 | `system` / `serif` / `mono` |
| `radius` | 整数 | `0`–`8` |
| `show_network`, `show_host`, `show_tags`, `show_remark` | 开关 | 开/关 |

声明口径：

- `type` 只能是 `managed`；`items` 最多 12 项，键不重复，未知键（例如 `resources`、`css`）直接拒绝。
- `name` 可选，是后台表单标题：写纯文本原样展示，写 `theme.` 开头的内置文案键则按后台语言翻译；
  省略时用内置标题。
- `options` 可选，只能收窄该键的合法取值且不重复；省略等于全部取值。`number`/`switch` 键不能写 `options`。
- 站点没有配置的字段继续用 `theme.json` 里的作者取值；作者不用在清单里重复默认值。
- 后台表单只渲染声明过的字段，保存的就是这些字段；`{}` 恢复包内默认值。
- 只支持 `managed`。`raw`（主题自带配置 HTML）和 `redirect`（跳第三方配置页）会把站点所有者
  的会话交给主题或第三方页面，Beat 不实现。
- 脚本主题（`runtime: "sandboxed"`）不能声明 `configuration`，它们的界面和配置都由自己接管。

后台表单的标题与取值标签都能落在内置文案键上（下表），因此同一份包在五种后台语言下都有对应的
标题；省略 `name` 时用的就是标题键的文案。写了表里没有的 `theme.` 键不会被拒绝，但界面会原样显示
这个键名，照抄下表最稳妥。

| 键 | 表单标题键 | 取值标签键 |
| --- | --- | --- |
| `overview` | `theme.overview_layout` | `theme.cards` / `theme.table` / `theme.tiles` / `theme.list` |
| `detail` | `theme.detail_layout` | `theme.balanced` / `theme.compact` / `theme.spotlight` |
| `density` | `theme.density` | `theme.comfortable` / `theme.compact` |
| `font` | `theme.font` | `theme.font_system` / `theme.font_serif` / `theme.font_mono` |
| `radius` | `theme.radius` | ——（整数输入，没有取值标签） |
| `show_network` / `show_host` / `show_tags` / `show_remark` | 与键同名的 `theme.show_*` | ——（开关，显示开/关） |

清单里已有声明时，站点能覆盖的键以清单为准；没有声明的旧主题保持历史四字段行为。

## 5. 安装、预览、配置和恢复

1. owner 打开 `/admin/settings` 的主题区，上传 ZIP 或安装示例。安装不自动激活。
2. 点击“预览”，检查桌面/手机、浅色/深色以及节点详情。手机预览使用真实窄视口。
3. 在右侧调整主题声明过的显示字段（布局、密度、圆角、字体或内容开关）。预览中的更改不会保存；
   点击“保存配置”并确认才保存。
4. 点击“激活”并确认，将主题用于访客页面。活动主题的配置保存后也会对访客生效。
5. “回滚”返回前一个主题；没有前包时回到内置页面。“恢复内置主题”直接恢复 Beat 默认页面。
6. “导出”下载原始、经过校验的 ZIP，不把站点覆盖配置写进包。发布配置变更时应同步修改源码再打包。

预览只读取主题描述和公开快照，不创建激活记录。上传、导出、激活、保存、删除及恢复需要
owner 和最近本地重认证；后台收到重认证要求后会就地收集密码/可选 TOTP 并重试原操作。
读取或校验失败的主题会显示内置页面。脚本超时/协议失败触发的回退仅影响当前访问者，
不会擅自改变全站主题；全站恢复由 owner 执行。活动包不能删除。

管理接口（均在 `/api/v1` 下）：

| 方法与路径 | 用途 |
| --- | --- |
| `GET /themes/active` | 匿名可读的活动描述，包含运行方式和已解析的声明式定义 |
| `GET /admin/themes/{id}/preview` | owner 只读预览描述，不激活 |
| `PUT /admin/themes/{id}/configuration` | 保存站点覆盖对象，键受清单 `configuration` 约束；`{}` 重置 |
| `POST /admin/themes/{id}/activate` | 激活安装包 |
| `POST /admin/themes/rollback` | 回到上一包或内置页 |
| `POST /admin/themes/built-in` | 明确恢复内置页 |

上传为原始 ZIP 请求体，`Content-Type: application/zip`，不是 multipart/form-data。
资源 URL 由描述中的 `archive_digest`、`sandbox_entry` 和 `preview_url` 提供，勿硬编码本地数据目录。

## 6. 脚本主题与 SDK

脚本包使用 `runtime: "sandboxed"`，包含**且只包含一个 HTML 文件**，位置可以是 `dist/index.html`。
随仓库发布的 `themes/pulse` 是一个可直接安装的完整脚本主题示例，可作为骨架复制：
`cp -R themes/pulse my-theme`。
入口必须引用包内的外部 JS/CSS；不允许内联脚本、事件属性、表单、iframe、重定向、SVG 或外部资源 URL。
ES module 的相对导入和包内 WOFF2 字体可用。所有引用文件都应进入清单。

将 `themes/sdk/bridge.js` 复制到自己的包中，例如 `dist/beat-sdk.js`。SDK 公开：

| 接口 | 行为 |
| --- | --- |
| `connectBeatTheme(render)` | 注册监听并响应协议 2 的握手；每次公开快照调用 `render(snapshot)` |
| `client.navigate(path)` | 请求返回首页或打开当前快照可见节点；成功发送返回 `true` |
| `client.reportError(code?)` | 发送 `render_failed`、`unsupported_data` 或 `runtime_error`；默认第一项 |
| `client.destroy()` | 移除监听并清空实例状态；组件卸载时调用 |

最小入口 `dist/index.html`：

```html
<!doctype html>
<html lang="zh-CN"><head><meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="theme.css"><script type="module" src="main.js"></script></head>
<body><h1 id="title"></h1><p id="state"></p><main id="nodes"></main></body></html>
```

对应 `dist/main.js`：

```js
import { connectBeatTheme } from './beat-sdk.js';
const client = connectBeatTheme((snapshot) => {
  document.documentElement.lang = snapshot.locale;
  document.documentElement.dataset.theme = snapshot.theme;
  document.querySelector('#title').textContent = snapshot.site.site_title;
  document.querySelector('#state').textContent = snapshot.state.nodes === 'ready' ? '' : snapshot.state.nodes;
  const elements = snapshot.nodes.map((node) => {
    const button = document.createElement('button');
    const cpu = Number.isFinite(node.metrics.cpu) ? `${node.metrics.cpu.toFixed(1)}%` : '--';
    button.textContent = `${node.alias || node.name} · ${node.status} · CPU ${cpu}`;
    button.onclick = () => client.navigate(`/node/${node.id}`);
    return button;
  });
  document.querySelector('#nodes').replaceChildren(...elements);
});
```

提供 `dist/theme.css`，例如 `body { font-family: sans-serif; padding: 1rem; }`，并为深色、手机和
键盘焦点补齐样式。脚本控制节点详情的呈现，应根据 `snapshot.route.kind` 展示详情及返回按钮；
历史曲线来自 `snapshot.history.series`。完整字段、单位、消息格式和握手见同目录的
[主题隔离协议](theme-sandbox-contract.md)。SDK 会校验父窗口来源、版本和实例，限制导航，并将渲染异常转换为通用错误码。

公开节点详情使用后台发布的公开/节点指标模板，提供时间范围、暂停、检查和 CSV/JSON 导出，
编辑操作只在后台提供。脚本主题启用时，外层仍会按相同公共模板查询，最多两路并发。
历史点的 `value: null` 表示缺口，不能变成 `0` 或连线；时间单位为 Unix 秒。
`history.series_info[metric]` 标明实际 `aggregation` 和 `unit`，尤其注意 counter 的 `rate` 单位。
同一指标按模板顺序选择首个成功系列；快照只合并相同成功时间范围的数据，失败时保留原范围并报告状态。
未查询的指标不出现历史键，不能把缺少历史当作成功的空结果。

SDK 的 `render` 必须快速返回；避免大规模同步计算或无限循环。主题无法直接访问后台 DOM、
cookies、localStorage、API、外网、弹窗或 worker。iframe 的安全来源是不透明的；不要依赖同源权限。
直接打开入口 HTML 也会受响应级 sandbox 限制。

## 7. 公开数据和资源限制

快照含站点公开信息、语言、明暗模式、可见分组/节点、资源与流量指标、节点详情历史，
以及站点允许时的公开网络质量摘要。隐藏节点、关闭显示的 IP、私有备注、SSH 密钥、
agent Token、会话、脚本采集命令和探测目标不会提供给主题。缺失或非有限样本被省略，
`0` 是有效值，不能用 `value || '--'` 判断缺失。
节点可携带 `telemetry_at`、`telemetry_age_seconds` 与 `telemetry_state`（fresh/delayed/stale/unavailable）；
这些字段描述实际收到的遥测数据，新鲜度不能由连接在线状态推断。不可用时前两项为 null。

包资源可匿名读取。不要把密码、API Key、SSH 文件或其他私密内容放入源码包或配置。
协议 2 只推送已有页面数据，不提供任意查询接口、PromQL 或新的 agent 采集能力。

| 资源 | 限制 |
| --- | --- |
| ZIP / 解压合计 | 25 MiB / 64 MiB；最多 512 项，压缩率上限 100 |
| 清单 / 声明式定义 | 256 KiB / 64 KiB |
| 单个文本、代码 / 图片、字体 | 8 MiB / 12 MiB；图片最多 16,777,216 像素 |
| 文件类型 | UTF-8 JSON/JS/CSS/HTML、PNG/JPEG/WebP、WOFF2；类型须匹配签名 |
| 路径 | 相对路径，最多 240 UTF-8 字节、8 层；禁止隐藏段、`..`、反斜杠、链接及特殊文件 |
| 快照 | 最多 1 MiB、1000 可见节点、200 分组/网络任务、64 历史系列、每系列 1024 点 |
| 握手 / 保活 | 5 秒内 ready；快照随数据变化及每 5 秒心跳更新 |

超限明确回退到内置页，不静默截掉一部分节点。资源被服务端校验大小、哈希和允许的类型后才提供。

## 8. 验证、分发和排错

修改示例后，运行 `bash scripts/build-themes.sh` 可重建十套分发 ZIP、预览图、SDK 和下载版文档。
前端构建会通过 `webui/package.json` 的 `prebuild` 自动执行同一脚本，因此 `npm run build` 的产物
（`dist/themes/`）始终带着十套可安装主题、预览图、SDK 和三份下载版文档；构建前端因此需要 Go 工具链。
容器与 CI 走同一条路径：`Dockerfile` 的前端阶段复用后端阶段构建好的 packer 并设置
`BEAT_THEME_BIN`，`scripts/check.sh frontend` 会读 `webui/src/lib/theme-examples.ts` 的后台示例列表，在构建后逐一确认每套 ZIP 与预览图（外加 SDK 与三份文档）确实进入静态目录。
十套分发 ZIP 的当前摘要（可由源码逐字节复现）：`slate` `5e3056f1…24b472`、`operations` `8198b705…53525a38`、`canvas` `05a92365…5bf749`、`graphite` `f2d44ef9…457c52a`、`aurora` `ba3979fb…4bd8a5`、`terminal` `5724fdbc…d67326`、`paper` `8aa69ce7…6125dc`、`midnight` `880b19d2…b5ff84`、`solar` `70a0ad70…8d96797`、`pulse` `a3508c0a…c69712`。`go test ./cmd/theme/` 会在十套源码上重新执行 `pack` 与 `validate`，核对声明式九套彼此不同、十套预览图都是可解码且不小于 320x180 的 PNG，并额外校验沙箱包只有一个 HTML 入口、入口引用包内的 `theme.css`/`theme.js`、包内 `beat-sdk.js` 与 `themes/sdk/bridge.js` 逐字节一致；分发资产存在时还会逐字节比对 ZIP、预览图、SDK 和下载版文档，源码改动后忘记重建会直接失败，资产缺失时只记录提示。
源码与 `themes/LICENSE` 是维护入口；ZIP 和 `webui/public/themes/` 是生成的分发资产。
示例和 SDK 使用 MIT，归档中的 `license.json` 携带完整许可。不要把 Beat 应用或第三方依赖的许可视为自动相同。

```bash
go test ./internal/theme ./cmd/theme
node --experimental-test-coverage --test themes/sdk/bridge.test.mjs
```

需要复验实际渲染时跑一键门禁即可：它把前端构建到临时目录，用 Go fixture 起停三套用例
（合成数据、临时 SQLite，不使用运行中的部署），再逐套跑浏览器断言并把截图写到输出目录。
Playwright 来自 `webui` 的 devDependency（`npm ci` 后可用），浏览器缺省用自带的 headless shell。

```bash
npm --prefix webui ci
bash webui/scripts/run-theme-browser-gates.sh /tmp/beat-theme-shots
```

只调试单套主题时，也可以手工起 fixture 并把 URL 交给对应脚本（脚本按自身位置解析 Playwright）：

```bash
npm --prefix webui run build -- --outDir /tmp/beat-theme-static
BEAT_DECLARATIVE_BROWSER_FIXTURE=1 BEAT_THEME_BROWSER_STATIC=/tmp/beat-theme-static \
  go test -v -timeout=11m ./internal/api/handler -run '^TestDeclarativeThemeBrowserFixture$'
node webui/scripts/theme-declarative-smoke.mjs http://127.0.0.1:PORT /tmp/beat-theme-shots
```

脚本先核对 fixture 标识，再验证三套布局、明暗/手机、隐私、预览、配置和内置恢复，最后关闭测试服务。
截图使用合成示例节点；生产主题预览使用当前站点的公开节点。

常见问题：包被拒绝时先执行 `validate`，核对 JSON、清单、路径和配色对比度；`pack` 已处理哈希，
不要在打包后直接编辑 ZIP。脚本空白或回退时检查 ready 时限、协议 2、相对导入、CSP 和通用错误码。
安装提示版本不兼容时，检查 `beat-server --version`。开发制品应注入与源码对应的明确版本；
默认 `dev` 按 `0.0.0` 比较，不会绕过 `minimum_beat_version` 门禁。
预览无节点时检查节点公开开关；IP 不显示时检查站点 IP 开关；历史为空时区分未采集、加载中和查询失败。
更换版本后仍看见旧主题时确认已安装的具体版本已激活，或用“恢复内置主题”验证站点自身可用。

市场发布使用 `beat-theme market keygen/identity/publish`，自动生成公钥身份、目录到期时间、双摘要与签名。
完整命令和分发配置见 [主题市场发布指南](theme-market-publishing.md)。

## 9. 字段速查表

打包与安装只读下面四个 JSON 片段；未知字段一律拒绝。逐字段的完整行为见第 3、4 节。

**`beat-theme.json`**（包清单，`files` 由 `pack` 重写）

| 字段 | 必需 | 取值（默认） |
| --- | --- | --- |
| `schema_version` | 是 | 固定 `1` |
| `id` | 是 | 3–64 个 ASCII 字符，仅小写字母、数字、`-`、`_` |
| `name` / `description` / `author` / `license` | 是 | 非空文本；`license` 由作者选择并附带 |
| `version` | 是 | 三段数字 `主.次.修订`，可带 `-`/`+` 后缀，≤ 64 字符 |
| `runtime` | 是 | `declarative` 或 `sandboxed` |
| `minimum_beat_version` | 是 | 三段数字版本，含边界 |
| `maximum_beat_version` | 否 | 三段数字版本；省略表示不设上限 |
| `preview` | 是 | 包内 PNG/JPEG/WebP 相对路径，必须出现在 `files` |
| `homepage` / `release_notes` | 否 | 文本 |
| `supported_locales` | 否 | 语言标识数组，不会自动翻译作者文本 |
| `configuration` | 否 | 站点可配置字段声明，见第 4.1 节 |
| `files` | 是 | `{path,size,sha256}` 数组；源码写 `[]`，`pack` 会填满 |

**`theme.json`**（声明式定义，≤ 64 KiB；站点可覆盖 `overview`/`detail`/`density`/`radius`，以及作者在 `configuration` 里声明的键，见第 4.1 节）

| 字段 | 必需 | 取值（默认） |
| --- | --- | --- |
| `schema_version` | 是 | 固定 `1` |
| `overview` | 是 | `cards` / `table` / `tiles` / `list` |
| `detail` | 是 | `balanced` / `compact` / `spotlight` |
| `density` | 是 | `comfortable` / `compact` |
| `radius` | 是 | `0`–`8` 整数，px |
| `font` | 否 | `system` / `serif` / `mono`（默认产品字体） |
| `resources` | 否 | `cpu` / `memory` / `disk`，1–3 项且不重复（默认三项全展示） |
| `show_network` / `show_host` / `show_tags` / `show_remark` | 否 | `true`（默认）或 `false`；只有显式 `false` 才隐藏 |
| `light` / `dark` | 是 | 各含全部 24 个调色板令牌，颜色为 `#RRGGBB` |

**`license.json`**（随包说明，服务端不解析，只在安装时进入 `files` 清单）

| 字段 | 必需 | 取值 |
| --- | --- | --- |
| `license` | 是 | 许可名称或 SPDX 标识符，与 `beat-theme.json` 保持一致 |
| `copyright` | 是 | 一行版权声明 |
| `text` | 是 | 完整许可正文，UTF-8，换行写成 `\n` |

**`configuration`**（清单里的自声明配置，可选）

| 字段 | 必需 | 取值（默认） |
| --- | --- | --- |
| `type` | 是 | 固定 `managed` |
| `items[]` | 是 | 最多 12 项，键不重复 |
| `items[].key` | 是 | `overview` / `detail` / `density` / `font` / `radius` / `show_network` / `show_host` / `show_tags` / `show_remark` |
| `items[].name` | 否 | ≤ 64 字符；纯文本，或 `theme.` 开头的内置文案键 |
| `items[].options` | 否 | 只能收窄该键的合法取值且不重复；仅枚举键可写 |

**站点覆盖配置**（`PUT /admin/themes/{id}/configuration`，≤ 16 KiB）：只接受主题声明过的键。
没有 `configuration` 的旧主题仍只接受 `overview`、`detail`、`density`、`radius`；取值与上表相同，
声明式主题之外不生效；`{}` 恢复包内默认值。

## 10. 常见拒绝原因自查清单

`init`、`pack`、`validate`、市场发布和后台安装走同一套生产校验器，报错文本只有下列几类。按顺序自查通常一次定位：

| 现象 / 报错 | 常见原因 | 处理 |
| --- | --- | --- |
| `theme: archive is invalid: invalid ZIP` | 文件不是 ZIP，或传输中被截断 | 重新 `pack`，再用 `validate` 复验整个文件 |
| `theme: manifest is invalid`（含配置声明被拒） | 配置声明里 `type` 不是 `managed`；键未知或重复；`items` 超过 12 项；`options` 超出该键合法取值、重复，或写在 `number`/`switch` 键上；作者自己写了 `items[].type`；脚本主题声明了 `configuration` | 对照第 4.1 节删掉多余字段，只声明键、`name` 与合法 `options` |
| `theme: manifest is invalid` | 未知字段；`schema_version` 不是 `1`；`id` 含大写或非法字符；`version` / `minimum_beat_version` 不是三段数字；`preview` 不是图片扩展名；`files` 漏项或重复；JSON 根对象后还有内容 | 对照第 3、9 节逐字段核对，不要手改 `pack` 生成的 `files` |
| 大小或哈希不匹配 | 打包后编辑过 ZIP 内容或清单 | 重新 `pack`，不要在打包后改动包内文件 |
| `theme: package identity or digest conflict` | 同一 `id` + `version` 或同一归档摘要已安装 | 递增 `version` 后重新打包 |
| `theme: runtime is not compatible` | 包要求的版本超出当前服务端范围 | 核对 `beat-server --version` 与 `minimum_beat_version` |
| 对比度被拒 | 八组背景/前景低于 4.5:1，或 `ring`、`chart-1`…`chart-5` 相对 `card` 低于 3:1；颜色不是 `#RRGGBB` | 调整色值，或改用 `init --accent` 生成的达标调色板 |
| 调色板令牌数量不对 | 少了或多了一个令牌 | `light`、`dark` 必须各含 24 个令牌，缺一个就整体拒绝 |
| 打包提示多余文件 | 源码目录里有 `.git`、`node_modules`、README 或临时 ZIP | 移出主题目录后再 `pack` |
| 沙箱主题被拒 | 多于一个 HTML 入口；入口内联脚本或引用外部 URL；包内文件未进清单 | 全部使用相对引用，CSS/JS/SDK 都放进 `dist/` |
| 打包正常但预览空白 | 硬编码资源 URL；ready 超过 5 秒；协议版本不符 | 用描述中的 `archive_digest` / `sandbox_entry` 构造 URL，并检查 SDK 握手 |
