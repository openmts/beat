# Notification v10 后端验证

日期：2026-09-15。对应 [通知实现契约](notification-core-contract.md)。
验证使用临时 SQLite、测试密钥和本机 HTTP/SMTP 接收器；本次没有部署、发送真实通知或执行 Git 提交。

## 结果

| 检查 | 结果 |
| --- | --- |
| `notification` race/shuffle | 通过 |
| `applicationsecrets` race/shuffle | 通过 |
| `notificationqueue` race/shuffle，包含 10,000 条投递与运行中重启 | 索引修复后复验中 |
| 备份、handler、API、server 的通知/密钥/渠道/恢复定向 race/shuffle | 通过 |
| 通知模板 fuzz，2 个 worker、5 秒 | 通过，10,910 次执行 |
| 相关包 `golangci-lint` / `go vet` | 通过，lint 0 个问题 |
| 服务端、agent 的 `go build -trimpath` | 通过 |
| 修改范围的 `goimports-reviser` / `git diff --check` | 通过 |

覆盖率：`notification` 91.7%、`notificationqueue` 91.4%、
`applicationsecrets` 91.1%。覆盖统计独立执行，排除重复的 10,000 条负载用例；
负载用例由完整 queue race/shuffle 单独验证。
通知渠道和投递 handler 两个新增文件的定向覆盖率均为 100%；
通知 backfill 与 resource-source 两个文件分别为 94.0% 和 92.5%。

## 本轮修正与检查范围

- 将租约提交和请求正文编码拆成短函数，明确接收可忽略的字符串写入返回值。
- race 首轮在构造 10,000 条队列时超时。查询计划确认回执按 `message_id`
  查询却扫描渠道索引；v10 新增 `(message_id, channel_id)` 索引，回归测试要求
  使用消息索引查找，并覆盖原始投递与重试记录。
- 核对了配置版本和权限的事务内复核、密钥保留/清除与预览脱敏、租约比较更新、
  失败通知恢复、限量清理重试链、v3 备份中的密文与过期租约恢复。
- 本文记录后端子任务结果；全仓库 CI、前端验收和部署由主集成任务统一收口。

主要验证命令（Go 工具显式使用 `env -u GOROOT`）：

```bash
go test -race -shuffle=on -count=1 ./internal/notification ./internal/notificationqueue ./internal/applicationsecrets
go test -race -shuffle=on -count=1 ./internal/backup ./internal/api/handler ./internal/api ./cmd/server -run 'Notification|ApplicationSecret|Channel|Operability|Restore'
go test ./internal/notification -run '^$' -fuzz FuzzDeclarativeTemplateNeverExecutesOrExceedsLimit -fuzztime=5s -parallel=2
golangci-lint run --allow-parallel-runners ./internal/notification ./internal/notificationqueue ./internal/applicationsecrets ./internal/api/handler ./internal/api ./cmd/server
go vet ./internal/notification ./internal/notificationqueue ./internal/applicationsecrets ./internal/api/handler ./internal/api ./cmd/server
```
