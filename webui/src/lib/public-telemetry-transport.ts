// 匿名计数使用独立的 fetch：无凭据、无重试、无队列，失败一律忽略。
export async function reportPublicInteraction(event: string, surface: string): Promise<void> {
  try {
    await fetch("/api/v1/public/interactions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ event, surface }),
      credentials: "omit",
      keepalive: true,
    })
  } catch {
    // 遥测失败绝不能阻塞或回滚用户操作。
  }
}
