# Agent 更新运行时

默认关闭。当前仅支持 Linux amd64/arm64、由 Beat 安装器管理的固定 systemd 服务和独立 binary 制品。容器交给外部管理器；缺少签名安装证据、权限不足或存在未知 systemd drop-in 时保持 manual-only。

## 首次启用

先由管理员安装可信的、包含更新命令的当前版本，再从该已安装程序执行：

```sh
sudo /opt/beat-agent/beat-agent service enable-updates \
  --config /etc/beat-agent/agent.json \
  --manifest /secure/installed-manifest.json \
  --trust /secure/rollout-trust.json \
  --artifact linux-amd64
```

- manifest 是当前安装版本的 `SignedManifest` JSON：`payload` 与 `signature` 使用 base64；payload 必须是发布方签名的 canonical manifest。
- trust 是带外取得的 Ed25519 公钥数组，字段为 `KeyID`、`PublicKey`（base64）、`NotBefore`、`ExpiresAt`、`Revoked`。输入文件须为当前安装账户拥有的私有普通文件（0600）。
- 手工安装还须指定已存在节点的 `--node-id`；有 enrollment receipt 时按原凭证与 Server 绑定校验，不允许改成另一节点。
- 已安装入口、签名制品及正在运行的 CLI 内核镜像必须精确匹配，内嵌运行版本也必须与 manifest 一致。旧安装记录中的自报 SHA256 不足以启用更新。
- 命令只新增本地 `updates.enabled/node_id`，保留原身份、Server、监控和远程操作配置。

## 执行与恢复

`beat-agent-update.service` 使用独立的 `beat-agent-update-helper`，与主 Agent 分属不同 cgroup。Agent 轮询 HTTPS lease、保存私有 assignment；helper 下载和发布。主服务启动失败时，helper 仍能按固定期限回滚。

下载按初次接受分配时刻累计最多 10 分钟；每块最多 4 MiB。遵守 Server 给出的 `ExpiresAt`，不续租、不重置下载预算。ServerSchema 仅取已认证 lease，不能从 manifest 自背书。候选须通过签名、平台、精确大小和 SHA256 校验；复制保留旧版本后，原子覆盖入口。

签名 receipt 与 fsync journal 支持跨启动校验。新进程须验证自身内核镜像，只有完整、匹配的报告 ACK 才产生健康证据；计时器不能制造健康样本。PID/启动时间改变会重置本地连续窗口，并结合 systemd 自动重启计数上报。本地窗口按报告间隔适配；启动与观察预算默认合计 3 分钟，最多 5 分钟。

本地状态位于安装目录的 `.beat-agent-update`（0700，普通记录 0600），不存 Agent token。排查可查看两个固定服务的状态及 journal 日志中的稳定错误码；不要公开私有 assignment 中的 nonce。将本地 `updates.enabled` 改为 `false` 并重启 Agent，helper 仍会恢复已有事务。

## 生成签名清单与信任锚

`beat-cli release` 是本地命令组，只读写文件，不连接服务端、不使用服务令牌。

```sh
./beat-cli release keygen --key-id beat-release-2026 \
  --key /secure/release.key --trust /secure/rollout-trust.json --server-trust /secure/server-rollout.json
./beat-cli release sign --spec release.json --key /secure/release.key --out /secure/signed-manifest.json
```

- `/secure/release.key`：单行 base64 的 32 字节 Ed25519 种子（0600），只留在签名侧。
- `/secure/rollout-trust.json`：给 Agent 的信任数组，字段为 `KeyID`、`PublicKey`（base64）、`NotBefore`、`ExpiresAt`、`Revoked`。
- `/secure/server-rollout.json`：`BEAT_AGENT_ROLLOUT_CONFIG` 的 `{schema_version, enabled, keys[], sigstore}` 文档。
- `keygen` 不覆盖既有文件；`sign` 可覆盖上一次清单输出。

`release.json` 只描述发布意图，`size` 与 `sha256` 由工具从真实文件计算：

```json
{
  "version": "0.2.0", "commit": "<40 位小写十六进制 git 提交>", "channel": "preview",
  "built_at": "2026-10-17T07:00:00Z", "expires_at": "2026-11-17T00:00:00Z",
  "minimum_server_schema": 28, "minimum_protocol": 2, "maximum_protocol": 2,
  "signing_key_id": "beat-release-2026",
  "release_notes": { "path": "docs/release-notes.md" },
  "artifacts": [
    { "id": "linux-amd64", "os": "linux", "arch": "amd64", "kind": "binary",
      "path": "dist/beat-agent", "entrypoint": "beat-agent", "support": "preview",
      "sbom": { "path": "dist/beat-agent.sbom.json" }, "provenance": { "digest": "<64 位小写十六进制>" } }
  ]
}
```

- `release_notes` 与每个产物的 `sbom`、`provenance` 都可以二选一：给 `path` 由工具计算 SHA-256，或直接给 64 位小写十六进制 `digest`。
- 产物必须是普通文件且不超过 512 MiB；`kind` 取 `binary`/`archive`/`external_manager`，`support` 取 `supported`/`preview`/`build-only`/`unsupported`。
- 写出前先执行一次 `VerifyManifestSignature` 自检，不合格的清单直接失败，不会产出无法导入的文件。

### 服务端信任模式

`BEAT_AGENT_ROLLOUT_CONFIG` 指向的配置文档必须显式声明 `signature_mode`：

- `sigstore`（缺省）：要求完整 Sigstore 策略（`certificate_identity`、`release_workflow`、
  `oidc_issuer` 与信任根文件）；
- `pinned_ed25519`：只用配置里列出的固定 Ed25519 公钥做信任锚，供没有 Sigstore 信任锚的
  离线实例使用。该模式只接受固定公钥签名，携带 Sigstore 证据的导入按签名失败拒绝，
  不把未校验的证据记成已校验。

同一份配置不允许既列公钥又提供 Sigstore 策略块：含糊组合在启动时直接失败，避免运维
以为两种锚点都在生效。`beat-cli release keygen` 产出的服务端配置会显式写出
`signature_mode: pinned_ed25519`。

配置文件要放在服务账户可读的位置。若目录本身是 0700 root-only（例如 `/etc/beat-preview`），
服务账户无法穿越，必须改用 `/var/lib/<service>/` 下的路径。

## 验证边界

独立 systemd fixture 已验证：签名目标无法启动，helper 保持存活，旧入口和旧服务恢复。该测试只控制随机命名的两个测试单元，不使用真实 Agent 配置或业务端口。

2026-10-17 在预览宿主上确认这条限制是真实可达的：用安装器把 agent 装成 `beat-agent.service` 后
执行 `service enable-updates` 返回 `error: agent update requires manual management`，原因是
`systemctl show beat-agent.service --property=DropInPaths` 返回
`/usr/lib/systemd/system/service.d/10-timeout-abort.conf`（`systemd-259.9-1.fc44` 自带，
只设 `TimeoutStopFailureMode=abort`）。

**所有者决策（方案 A，v139 起）：接纳可证明良性的系统级 drop-in。** 准入规则见
`internal/agentinstall/dropin.go`：

- 只接受 vendor 目录（`/usr/lib/systemd/system`、`/usr/local/lib/systemd/system`、`/lib/systemd/system`）
  下 `<单元或类型>.d/*.conf` 形式的绝对路径；
- 文件必须是非符号链接、当前用户属主（安装时为 root）、组/其他不可写、≤16 KiB；
- 内容只允许 `[Unit]`/`[Service]` 下的 `TimeoutStopFailureMode`（abort/kill/exit）、
  `TimeoutStopSec`、`TimeoutAbortSec`（数字加单位或 `infinity`）；
- 出现 `Exec*`/`User`/`Group`/`Environment*`/`Restart*`、未知键、缺少节、越界数值或运算符字符
  一律仍按未知覆盖拒绝。

v140 起节点表（宿主 uname，如 `x86_64`）与发布清单/自报能力（GOARCH，如 `amd64`）比较前统一口径，
避免真实主机被判 `platform_changed`；策略里的 `max_restarts` 是「额外」重启预算，比较时补回升级
自身那一次重启（v141）。

已知吞吐边界：升级分片固定 4 MiB、单次分片请求 30s 超时，等价于要求约 140 KB/s 以上的持续吞吐。
2026-10-17 演练中经 Cloudflare 入口实测约 116 KB/s，分片永远超时、下载无法推进（helper 反复
`update_failed`，`download.part` 停在同一个 4 MiB 边界）。演练改用本机 https 反代后正常。
是否改为自适应分片或按进度而非单请求计时，留待后续决策。
