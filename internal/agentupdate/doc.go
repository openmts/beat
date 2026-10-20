// Package agentupdate 提供独立的 Linux Agent 更新事务核心。
//
// Config 的 ServerURL、Keys 和 Installed 只能由本地可信配置/安装元数据提供。
// 调用方不得从 assignment 或下载内容推导新的信任根、当前安装摘要或服务命令。
// Manager 必须是固定服务适配器；本包不执行 shell，也不配置 systemd。
//
// Apply 只发布经离线 Ed25519、平台、大小、SHA256 校验的独立 binary。
// 当前入口保留到原子 rename；previous.bin 保存上一已验证版本。私有 journal
// 记录持久化下载进度、发布意图、恢复状态与固定健康期限，不保存 Agent token。
//
// 集成方须在服务外提供有界 watchdog，启动/崩溃后调用 Recover，并由运行目标
// binary 的进程以可信 Installed 元数据构造 Updater，再提交 Observe 健康证据。
// Controller按真实报告ACK上报观测，Supervisor由独立helper服务驱动恢复。
// 安装CLI显式启用更新；agentinstall提供固定systemd适配与签名安装证明bootstrap。
// Restart 在崩溃恢复时可能重复调用，固定 Manager 必须允许幂等重启。
package agentupdate
