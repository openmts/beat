# GeoIP 运维

GeoIP 使用 SQLite v13；运行时与 backup v4 接线应随本批一起交付。
默认 provider 为 `disabled`，country/expiry/price 默认不公开。

- 推荐上传自有许可的 Country MMDB。上传为原始 `application/octet-stream`，
  最大 128 MiB，`If-Match` 为当前配置 revision。
- 固定管理入口为 `/api/v1/admin/settings/geoip`；其 `/mmdb`、
  `/mmdb/update`、`/mmdb/rollback` 操作要求 owner 与近期重新认证。
- 官方更新只访问 MaxMind 固定 HTTPS origin，核对 SHA-256 后验证归档和数据库；
  拒绝跨 origin 重定向。账号与 license key 仅用于此次请求，不持久化。
- GeoJS/IPinfo/ip-api Pro 启用必须独立确认第三方 IP 披露；只发送 public IP。
  DNS 查询仅由明确确认的 `managed_target` 模式触发。
- `observed` 来源取可信代理处理中间件的请求地址，只采集 active per-node 身份。
  Agent 的 `host` 不参与来源判定；来源密文和 HMAC 均不进入公开响应。
- 失败保留已有结果，90 天后显示 stale。关闭立即隐藏；purge 明确清除来源和衍生缓存。

托管资产为 `geoip/country.mmdb` 和 `geoip/country.mmdb.previous`，目录 0700、
文件 0600。`.publication.json` 和 `.previous.mmdb.pending` 是发布恢复临时状态，
不应手工编辑。启动时先恢复发布记录，再载入数据库。外挂只读 MMDB 不复制、不 chmod；
缺失时本地 provider readiness 降级。

备份集成必须通过 `WithBackupAssets` 保持发布互斥，复制固定清单并保存 checksum/build/size；
恢复时调用 `ValidateAsset`，不得将任意路径或归档链接当作 MMDB。

## 节点生命周期

`/api/v1/admin/settings/lifecycle` 管理站点日历时区和提醒阶段；
`/api/v1/admin/nodes/{id}/lifecycle` 管理供应商、套餐、价格和到期日。
`price_micros` 以十进制 JSON 字符串传输，避免浏览器丢失 int64 精度；
`expires_on` 是站点时区中的 `YYYY-MM-DD` 合同日期。

默认人工更新日期。启用 `calendar_auto_advance` 需要独立确认，只推进跟踪日期，
不付款、不联系供应商、不停止监控。月底和闰年保留原始锚点，单次最多推进 120 个周期，
超限保持原日期并报告错误。修改日期会产生新周期，旧提醒不会被复用。

`/api/v1/admin/nodes/{id}/lifecycle/events` 保留提醒和日期变更证据；未订阅渠道时明确
显示 `not_routed`。日期、事件、审计、通知消息和投递记录共享事务，失败整体回滚。
公开到期和价格各自独立控制，默认隐藏；私有备注永不进入公开节点或通知内容。
