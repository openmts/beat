# 发布 Beat 主题市场

市场是一个 HTTPS 目录和一把 Ed25519 公钥。Beat 根据公钥计算发布者身份，验证目录签名、到期时间和包摘要；用户在后台选择版本后安装，再单独预览和激活。

## 1. 生成发布密钥

在仓库根目录执行；发布归档内已带编译好的 `beat-theme`，需要自行构建时执行 `go build -o beat-theme ./cmd/theme`：

```bash
go run ./cmd/theme market keygen \
  --private-key ~/.beat-theme-keys/publisher.key \
  --public-key ./public-market/publisher.pub

go run ./cmd/theme market identity --public-key ./public-market/publisher.pub
```

新目录权限为 `0700`，新文件为 `0600`。私钥保存于发布目录之外；只分发公钥、目录、签名和主题 ZIP。
`keygen` 输出公钥和公共身份，不输出私钥。公共身份格式为 `sha256:` 加原始 32 字节公钥的 SHA-256 小写十六进制摘要。

此身份与各 Beat 实例自动生成的市场源 UUID 无关，同一发布者可以添加到多个实例。更换公钥意味着新的发布者身份。

## 2. 准备主题包和目录输入

```bash
go run ./cmd/theme pack --source ./themes/slate --output ./public-market/slate.theme.zip
go run ./cmd/theme validate --archive ./public-market/slate.theme.zip
```

创建 `public-market/entries.json`，归档路径相对于此输入文件解析：

```json
[
  {
    "archive": "slate.theme.zip",
    "package_url": "https://themes.example.com/slate.theme.zip"
  }
]
```

每个主题 ID 在目录中最多出现一次，通常列出当前可安装版本。空数组可发布空目录。单个目录最多 200 个主题。
同一 `(theme_id, version)` 的内容不可替换，更新内容时需增加版本号。

## 3. 生成签名目录

```bash
go run ./cmd/theme market publish \
  --private-key ~/.beat-theme-keys/publisher.key \
  --entries ./public-market/entries.json \
  --output ./public-market/catalog.json \
  --signature ./public-market/catalog.sig \
  --expires-in 24h
```

发布工具会复用服务端归档校验器，自动填入主题身份、版本、运行时和兼容范围，并计算 ZIP 原始字节的 `package_sha256`、包内原始 `beat-theme.json` 的 `manifest_sha256`。不要手动复制或改写这些摘要。

输出文件必须不存在，避免误覆盖密钥、输入文件或已发布制品。发布下一版时使用新的输出目录，验证后由静态服务原子切换。
默认有效期 24 小时，最长 7 天；签名目录包含必需的 `expires_at`。到期前需要重新发布签名目录，即使主题版本没有变化。

## 4. 配置 HTTPS 分发

提供 `catalog.json` 和 ZIP 的 HTTPS 下载地址，并让目录响应包含：

```text
Content-Type: application/json
X-Beat-Catalog-Signature: <catalog.sig 去除末尾换行后的内容>
ETag: <随目录原始内容变化的实体标签>
```

签名覆盖服务端实际收到的完整目录字节，包括空格和末尾换行。发布后再格式化 JSON 会使签名失效。
支持标准 `ETag` / `Last-Modified` 和 `304`；不要在内容改变后继续返回旧验证标签。

生产 Beat 仅访问公网 HTTPS 地址，最多接受三次重定向。每次连接都会重新解析并检查全部 DNS 结果，再直接连接已检查的数字 IP；TLS 证书与 SNI 仍按原域名校验。代理环境变量不影响此客户端。
目录上限为 2 MiB，ZIP 上限为 25 MiB；目录中的远程预览图片不会由后台页面直接加载。

## 5. 在 Beat 中添加和使用

进入“设置 → 主题 → 市场”，填写名称、目录 URL 和公钥。指纹字段可留空让 Beat 自动计算，也可填入发布者公布的指纹核对；不匹配会拒绝保存。

“刷新目录”会实际联系市场源。新下载、已验证缓存和源暂不可达的回退状态会分别展示，并显示验证时间、缓存到期时间和签名目录到期时间。
只有此前验签成功、尚未到期且距离验证不超过 15 分钟的目录可用于网络失败回退。签名或格式校验失败时不回退。
重启后只有数据库中的 ETag 不能证明目录有效，Beat 会重新获取完整目录。

安装请求绑定用户选中的 `version` 和 `package_sha256`。目录换版、撤下条目或源在下载期间被禁用、删除、换钥、换址时，安装会拒绝提交；刷新后重新选择即可。
升级会并排保留旧版本。安装、重装都不会自动激活；可先预览，再确认激活或回滚到最近可用的健康版本。

活动主题不可删除。删除成功后包立即退出列表，资源和导出立即不可用；文件隔离失败时页面会明确显示“已删除、待清理”并提供重试，启动时也会自动重试。
重新安装完全相同的 ZIP 会恢复原包 ID 和显示配置，不会自动激活。更改过 ZIP 字节的同名同版本包会被拒绝。

主题格式、样例和运行时协议见 [主题制作指南](theme-author-guide.md) 与 [沙箱协议](theme-sandbox-contract.md)。

## 6. 离线机房的自建市场

默认市场客户端只接受公网地址。自建市场监听私网、回环或保留地址时，在 Beat 服务端启动前设置：

```text
BEAT_THEME_MARKET_ALLOW_PRIVATE=true
```

这是显式的运维开关，缺省关闭；只有 `true`（大小写不敏感）才放通私网来源，写成其它值会让服务端启动失败而不是静默降级。开关打开后启动日志会记录一条 `theme market private sources are allowed`。

开关只放宽地址策略：仍然只走 HTTPS、每次解析只取一轮结果并只连接校验过的地址、保持原域名的 Host/SNI、最多三次重定向、目录 2 MiB 与 ZIP 25 MiB 上限，代理环境变量同样不生效。开关状态变化需要重启服务端。
