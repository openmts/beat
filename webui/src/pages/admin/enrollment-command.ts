const heredocDelimiter = "BEAT_ENROLL"

/**
 * 拼出「复制即可执行」的接入命令。秘密经标准输入进入 heredoc，不会落到 shell 历史；
 * 命令只在弹窗状态里存在，不写入 localStorage、URL 或日志。
 */
export function enrollmentCommand(origin: string, secret: string): string {
  // 明文 HTTP 需要显式开关；HTTPS 保持默认，不额外放宽传输策略。
  const transport = origin.startsWith("https://") ? [] : ["--allow-http"]
  const invocation = ["beat-agent enroll", `--server ${origin}`, ...transport,
    "--grant-stdin", `<<'${heredocDelimiter}'`].join(" ")
  return [invocation, secret, heredocDelimiter].join("\n")
}
