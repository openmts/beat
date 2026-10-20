import { useLocale } from "@/context/locale"

// 可选配置段（updates / accelerators / operations）的说明只服务后台节点接入页。
// src/context/agent-setup-translations.ts 会随入口分片进入公共外壳，新增条目会挤占
// beat-public-shell 的 250 KiB gzip 预算（webui/scripts/public-shell-plugin.ts），
// 因此按 src/components/metric-workspace/legend-text.ts 的写法把文案留在本模块。
// 字段名必须与 internal/agent/config.go、internal/agentupdate、internal/acceleratorcollect、
// internal/agentoperations 的 JSON 标签一致，internal/agent/config_reference_test.go 会校验漂移。
type Localized = readonly [string, string]

export type AgentOptionalField = { name: string; type: string; requirement: Localized; meaning: Localized }

export type AgentOptionalSection = { name: string; requirement: Localized; meaning: Localized; fields: AgentOptionalField[] }

export type ResolvedAgentOptionalField = { name: string; type: string; requirement: string; meaning: string }

export type ResolvedAgentOptionalSection = { name: string; requirement: string; meaning: string; fields: ResolvedAgentOptionalField[] }

const optional = ["可选", "Optional"] as const
const inSection = ["本段必填", "Required in the section"] as const
const whenEnabled = ["启用时必填", "Required when enabled"] as const
const whenCommands = ["命令开启时必填", "Required when commands are enabled"] as const
const withDefault = (value: string): Localized => [`可选；默认 ${value}`, `Optional; default ${value}`]

export const agentOptionalSections: AgentOptionalSection[] = [
 {
  name: "updates",
  requirement: optional,
  meaning: [
   "签名自动更新。该段通常由 beat-agent service enable-updates 写入，不要在手工安装时凭猜填写；agent 要求 server_url 是 HTTPS，并把信任材料存放在可执行文件旁的私有状态目录。",
   "Signed auto-update. It is normally written by beat-agent service enable-updates, so do not guess its values on a manual install; the agent requires an HTTPS server_url and keeps trust material in a private state directory beside the executable.",
  ],
  fields: [
   {
    name: "enabled", type: "boolean", requirement: inSection,
    meaning: [
     "填 false 或省略整段都表示关闭自更新。填 true 时必须有 node_id 且 server_url 为 HTTPS，否则 agent 拒绝启动。",
     "false, or omitting the whole section, disables self-update. true requires node_id and an HTTPS server_url; otherwise the agent refuses to start.",
    ],
   },
   {
    name: "node_id", type: "string", requirement: whenEnabled,
    meaning: [
     "升级平台为该节点签发的签名标识，形如 node-one：以字母或数字开头，只含字母、数字和 . _ + -，最多 128 个字符。填错会阻止启动。",
     "Signing identifier issued to this node by the rollout platform, for example node-one: it starts with a letter or digit and allows letters, digits and . _ + - up to 128 characters. A wrong value stops startup.",
    ],
   },
  ],
 },
 {
  name: "accelerators",
  requirement: optional,
  meaning: [
   "GPU / 加速器采集。默认关闭；可在此按节点开启，也可在后台「节点 → 加速器」下发采集策略，服务端策略优先。agent 不会安装驱动或厂商工具。",
   "GPU and accelerator collection. Off by default; enable it here per node or through the server-side policy at Nodes → Accelerators, which wins over this file. The agent never installs drivers or vendor tools.",
  ],
  fields: [
   {
    name: "enabled", type: "boolean", requirement: inSection,
    meaning: [
     "默认 false。开启但缺少 nvidia-smi 或 rocm-smi 时 agent 仍会启动，并上报工具缺失状态而不是启动失败。",
     "false by default. When no nvidia-smi or rocm-smi is available the agent still starts and reports the missing tool instead of failing.",
    ],
   },
   {
    name: "sample_interval_seconds", type: "integer", requirement: withDefault("30"),
    meaning: [
     "5 到 3600 秒。间隔越短 CPU 开销越大；超出范围或非整数会让 agent 拒绝启动。",
     "5 to 3600 seconds. Shorter intervals cost more CPU; out-of-range or non-integer values stop the agent from starting.",
    ],
   },
   {
    name: "maximum_devices", type: "integer", requirement: withDefault("32"),
    meaning: [
     "1 到 32。超过上限的设备会被跳过，并在上报里按截断计数。",
     "1 to 32. Devices beyond the limit are skipped and reported as truncated.",
    ],
   },
   {
    name: "metrics", type: "string[]", requirement: optional,
    meaning: [
     "取值只能是 gpu_utilization_percent、gpu_memory_used_bytes、gpu_memory_total_bytes、gpu_temperature_celsius、gpu_power_watts、gpu_power_limit_watts 的子集；省略表示全部采集。未知键或重复键会让 agent 拒绝启动。",
     "A subset of gpu_utilization_percent, gpu_memory_used_bytes, gpu_memory_total_bytes, gpu_temperature_celsius, gpu_power_watts and gpu_power_limit_watts; omitting it collects all of them. Unknown or duplicate keys stop the agent from starting.",
    ],
   },
  ],
 },
 {
  name: "operations",
  requirement: optional,
  meaning: [
   "远程运维开关。省略该段时服务端只能读取指标；启用后管理员可以按下面的策略在该节点执行命令和传输文件。仅 Linux 支持，其他平台填 true 会被拒绝。",
   "Remote operations switch. Without the section the server can only read metrics. Once enabled, administrators can run commands and transfer files on this node under the policy below. Linux only; other platforms reject enabled: true.",
  ],
  fields: [
   {
    name: "enabled", type: "boolean", requirement: inSection,
    meaning: [
     "默认 false。填 true 时必须提供绝对的 state_dir，否则 agent 拒绝启动。",
     "false by default. true requires an absolute state_dir, otherwise the agent refuses to start.",
    ],
   },
   {
    name: "state_dir", type: "string", requirement: whenEnabled,
    meaning: [
     "任务日志与租约的私有目录，必须填绝对路径且与配置文件、可执行文件、file_roots 互不重叠；agent 会以 0700 创建它。",
     "Private directory for operation journals and leases. It must be absolute and must not overlap the config file, the executable or any file_roots path; the agent creates it with 0700 permissions.",
    ],
   },
   {
    name: "max_concurrency", type: "integer", requirement: withDefault("1"),
    meaning: [
     "同时执行的任务数上限，也是上报给服务端的并发能力；填 0 会按 1 处理。",
     "Upper bound on concurrently running operations, and the concurrency advertised to the server. Zero is treated as 1.",
    ],
   },
  ],
 },
 {
  name: "operations.commands",
  requirement: optional,
  meaning: [
   "命令执行策略。require_cgroup 属于运行时探测结果，不能由配置文件设置。",
   "Command execution policy. require_cgroup is a runtime probe result and cannot be set from the config file.",
  ],
  fields: [
   {
    name: "enabled", type: "boolean", requirement: inSection,
    meaning: [
     "默认 false。开启后服务端可以下发命令片段；agent 必须由 root 启动，否则能力协商失败。",
     "false by default. Once enabled the server can deliver command snippets; the agent must run as root or capability negotiation fails.",
    ],
   },
   {
    name: "allow_ad_hoc", type: "boolean", requirement: withDefault("false"),
    meaning: [
     "默认 false，只允许已保存的命令片段；填 true 才允许临时命令。",
     "false by default, which only allows saved snippets; set true to accept ad-hoc commands.",
    ],
   },
   {
    name: "run_as_user", type: "string", requirement: withDefault("beat-ops"),
    meaning: [
     "命令的执行账户，必须是主机上已存在的账户；账户不存在时 agent 拒绝启动。",
     "Account used to run commands. It must already exist on the host; otherwise the agent refuses to start.",
    ],
   },
   {
    name: "allow_root", type: "boolean", requirement: withDefault("false"),
    meaning: [
     "是否允许以 root 执行。默认 false，命令始终降权到 run_as_user。",
     "Whether root execution is allowed. It defaults to false, so commands always drop to run_as_user.",
    ],
   },
   {
    name: "maximum_timeout", type: "string", requirement: withDefault("5m"),
    meaning: [
     "Go duration 字符串，必须是整秒，例如 30s、5m；超过该值的任务会被拒绝。",
     "Go duration string in whole seconds, for example 30s or 5m. Operations above this value are rejected.",
    ],
   },
   {
    name: "maximum_output_bytes", type: "integer", requirement: withDefault("262144"),
    meaning: [
     "单个任务的输出上限，超出部分会被截断后仍按上限上报。",
     "Output cap for a single operation; anything beyond it is truncated while the operation still reports up to the cap.",
    ],
   },
   {
    name: "working_root", type: "string", requirement: whenCommands,
    meaning: [
     "必须等于某个 file_roots 的 id，命令在该目录下执行。",
     "Must equal the id of one file_roots entry; commands run inside that directory.",
    ],
   },
   {
    name: "shell_path", type: "string", requirement: optional,
    meaning: [
     "解释命令片段的 shell 绝对路径，默认取平台 shell；该文件与 state_dir、配置、可执行文件互不重叠。",
     "Absolute path of the shell that interprets snippets, defaulting to the platform shell. It must not overlap state_dir, the config file or the executable.",
    ],
   },
   {
    name: "cgroup_root", type: "string", requirement: optional,
    meaning: [
     "cgroup v2 挂载点，默认 /sys/fs/cgroup，用于限制命令的内存与进程数。",
     "cgroup v2 mount point, /sys/fs/cgroup by default, used to limit command memory and process count.",
    ],
   },
   {
    name: "memory_bytes", type: "integer", requirement: withDefault("268435456"),
    meaning: [
     "每条命令的内存上限，取值 16 MiB 到 4 GiB（16777216 到 4294967296）。",
     "Memory cap per command, between 16 MiB and 4 GiB (16777216 to 4294967296).",
    ],
   },
   {
    name: "maximum_processes", type: "integer", requirement: withDefault("64"),
    meaning: [
     "每条命令的进程数上限，取值 1 到 1024。",
     "Process cap per command, between 1 and 1024.",
    ],
   },
  ],
 },
 {
  name: "operations.file_roots",
  requirement: optional,
  meaning: [
   "文件传输白名单，数组；每个根目录都必须已存在、权限为 0700 的绝对目录，且互不重叠、也不能覆盖 state_dir、配置文件、可执行文件或 shell。read 会启用 collect 能力，write 会启用 deploy 能力。",
   "File transfer allowlist as an array. Every root must be an existing absolute directory with 0700 permissions, must not overlap another root, and must not cover state_dir, the config file, the executable or the shell. read enables the collect capability and write enables deploy.",
  ],
  fields: [
   {
    name: "id", type: "string", requirement: inSection,
    meaning: ["根目录标识，commands.working_root 引用它；同一份配置内不能重复。", "Root identifier referenced by commands.working_root; it must be unique inside one config."],
   },
   { name: "name", type: "string", requirement: optional, meaning: ["后台显示的名称，便于管理员辨认。", "Display name shown to administrators."] },
   {
    name: "path", type: "string", requirement: inSection,
    meaning: ["已存在的绝对目录，不能是 /，也不能与 state_dir、配置、可执行文件和 shell 重叠。", "Existing absolute directory. It cannot be / and must not overlap state_dir, the config file, the executable or the shell."],
   },
   { name: "read", type: "boolean", requirement: withDefault("false"), meaning: ["允许从该目录读取文件。", "Allows reading files from the directory."] },
   { name: "write", type: "boolean", requirement: withDefault("false"), meaning: ["允许向该目录写入文件。", "Allows writing files into the directory."] },
   {
    name: "maximum_file_bytes", type: "integer", requirement: optional,
    meaning: ["单个文件的大小上限，单位字节；超过该值的读写会被拒绝。", "Per-file size cap in bytes; larger transfers are rejected."],
   },
   { name: "overwrite", type: "boolean", requirement: withDefault("false"), meaning: ["是否允许覆盖已存在的文件。", "Whether existing files may be overwritten."] },
   { name: "allow_executable", type: "boolean", requirement: withDefault("false"), meaning: ["是否允许传输带可执行位的文件。", "Whether files carrying the executable bit may be transferred."] },
  ],
 },
 {
  name: "diagnostics",
  requirement: optional,
  meaning: [
   "高级网络诊断执行开关。默认关闭；启用后服务端才可以在这台机器上执行运维已审批的 mtr 与 iperf3 作业。agent 只执行固定的绝对二进制与固定参数，从不使用 shell，也不接受任意目标地址。",
   "Advanced network diagnostics execution switch. Off by default. Once enabled the server may run operator-approved mtr and iperf3 jobs on this host. The agent only runs fixed absolute binaries with fixed arguments, never a shell, and never an arbitrary destination.",
  ],
  fields: [
   {
    name: "enabled", type: "boolean", requirement: inSection,
    meaning: [
     "默认 false。填 true 时必须提供 state_dir、至少一个二进制路径和非空的 allowed_targets，否则 agent 拒绝启动。",
     "false by default. true requires state_dir, at least one binary path and a non-empty allowed_targets, otherwise the agent refuses to start.",
    ],
   },
   {
    name: "mtr_path", type: "string", requirement: optional,
    meaning: [
     "mtr 可执行文件的绝对路径，例如 /usr/bin/mtr；留空表示不执行 MTR，也就不会上报该能力。",
     "Absolute path of the mtr binary, for example /usr/bin/mtr. Empty means MTR is not executed and the capability is not advertised.",
    ],
   },
   {
    name: "iperf3_path", type: "string", requirement: optional,
    meaning: [
     "iperf3 可执行文件的绝对路径，例如 /usr/bin/iperf3；留空表示不执行带宽测试。",
     "Absolute path of the iperf3 binary, for example /usr/bin/iperf3. Empty means bandwidth tests are not executed.",
    ],
   },
   { name: "mtr_version", type: "string", requirement: optional, meaning: ["上报给服务端展示的 MTR 版本，仅用于审计。", "MTR version reported for display and audit only."] },
   { name: "iperf3_version", type: "string", requirement: optional, meaning: ["上报给服务端展示的 iperf3 版本，仅用于审计。", "iperf3 version reported for display and audit only."] },
   {
    name: "state_dir", type: "string", requirement: whenEnabled,
    meaning: [
     "私有状态目录的绝对路径；agent 以 0700 创建它，尝试日志与执行工作目录都放在这里。",
     "Absolute path of the private state directory. The agent creates it with 0700 permissions and keeps attempt journals and the execution work directory there.",
    ],
   },
   {
    name: "poll_interval", type: "string", requirement: withDefault("5s"),
    meaning: ["Go duration 字符串，1s 到 1h，决定领取新作业的间隔。", "Go duration string between 1s and 1h that sets how often new jobs are claimed."],
   },
   {
    name: "allowed_targets", type: "string[]", requirement: whenEnabled,
    meaning: [
     "本机白名单，最多 16 条，只接受精确 IP 或已掩码 CIDR，例如 203.0.113.10 或 10.0.0.0/8；agent 不会在本地解析主机名。服务端审批结果必须同时落在这里才会真正发包。",
     "Local allowlist, up to 16 entries, accepting only exact addresses or already-masked CIDRs such as 203.0.113.10 or 10.0.0.0/8; the agent never resolves hostnames locally. Traffic is sent only when the server approval also falls inside this list.",
    ],
   },
   {
    name: "maximum_concurrent", type: "integer", requirement: withDefault("1"),
    meaning: ["同时执行的诊断数上限，取值 1 到 2。", "Upper bound on concurrently running diagnostics, between 1 and 2."],
   },
   { name: "allow_udp", type: "boolean", requirement: withDefault("false"), meaning: ["是否上报并允许 UDP 探测。", "Whether UDP probing is advertised and allowed."] },
   { name: "allow_reverse", type: "boolean", requirement: withDefault("false"), meaning: ["是否允许 iperf3 反向（-R）测试。", "Whether reverse (iperf3 -R) tests are allowed."] },
   {
    name: "allow_private_targets", type: "boolean", requirement: withDefault("false"),
    meaning: [
     "是否允许私网、CGNAT 与文档网段目标。白名单里出现这类地址时必须同时开启，否则 agent 拒绝启动。",
     "Whether private, CGNAT and documentation-range targets are allowed. It must be enabled together with such an allowlist entry, otherwise the agent refuses to start.",
    ],
   },
   {
    name: "maxima", type: "object", requirement: optional,
    meaning: [
     "本地上限，服务端只能收紧不能放宽；省略时用默认值。",
     "Local maxima that the server can only tighten. Defaults apply when omitted.",
    ],
   },
  ],
 },
 {
  name: "diagnostics.maxima",
  requirement: optional,
  meaning: [
   "诊断执行的本地数值上限。服务端策略与作业参数都不能突破这里，越界的能力上报会被拒绝。",
   "Local numeric caps for diagnostic execution. Neither server policy nor job parameters can exceed them, and any capability above them is rejected.",
  ],
  fields: [
   { name: "probes", type: "integer", requirement: withDefault("10"), meaning: ["MTR 探测次数上限，取值 3 到 20。", "MTR probe cap, between 3 and 20."] },
   { name: "hop_limit", type: "integer", requirement: withDefault("32"), meaning: ["MTR 跳数上限，取值 1 到 64。", "MTR hop cap, between 1 and 64."] },
   { name: "duration_seconds", type: "integer", requirement: withDefault("10"), meaning: ["iperf3 单次时长上限，取值 3 到 60 秒。", "iperf3 duration cap, between 3 and 60 seconds."] },
   { name: "parallel_streams", type: "integer", requirement: withDefault("1"), meaning: ["iperf3 并行流上限，取值 1 到 4。", "iperf3 parallel stream cap, between 1 and 4."] },
   { name: "bitrate_mbps", type: "integer", requirement: withDefault("100"), meaning: ["iperf3 带宽上限，取值 1 到 1000 Mbps。", "iperf3 bitrate cap, between 1 and 1000 Mbps."] },
  ],
 },
]

export function useAgentOptionalSections(): ResolvedAgentOptionalSection[] {
 const { locale } = useLocale()
 const index = locale === "zh-CN" ? 0 : 1
 return agentOptionalSections.map((section) => ({
  name: section.name,
  requirement: section.requirement[index],
  meaning: section.meaning[index],
  fields: section.fields.map((field) => ({
   name: field.name,
   type: field.type,
   requirement: field.requirement[index],
   meaning: field.meaning[index],
  })),
 }))
}

const optionalText = {
 title: ["可选配置段", "Optional sections"],
 description: [
  "以下四段可以整段省略，省略即关闭对应功能。字段名区分大小写，未列出的字段会让 agent 启动失败。",
  "All four sections may be omitted entirely, which disables the matching feature. Field names are case sensitive, and any field not listed here stops the agent from starting.",
 ],
} as const

export function useAgentOptionalText() {
 const { locale } = useLocale()
 const index = locale === "zh-CN" ? 0 : 1
 return { title: optionalText.title[index], description: optionalText.description[index] }
}
