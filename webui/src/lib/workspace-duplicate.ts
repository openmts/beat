/** 服务端看板标题上限；复制时同步截断，避免用户提交后才发现校验失败。 */
export const maxDashboardTitle = 120

/** 复制看板默认沿用原标题加后缀，空标题回落到后缀本身，最终截断到服务端上限。 */
export function duplicateDashboardName(title: string, suffix: string): string {
  const base = title.trim()
  const name = base ? `${base} ${suffix}` : suffix
  return name.slice(0, maxDashboardTitle)
}
