import type { DashboardDefinition } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：最多 8 个标签、每个不超过 64 字节、小写键且唯一。 */
export const maximumDashboardTags = 8
export const maximumDashboardTagBytes = 64
/** 输入框允许的原始长度：8 个标签各自加上分隔符与余量，避免输入被静默截断。 */
export const maximumDashboardTagsInputLength = maximumDashboardTags * (maximumDashboardTagBytes + 2)

const dashboardTagPattern = /^[a-z0-9][a-z0-9_-]{0,63}$/

/** parseDashboardTags 把逗号或空白分隔的输入折成小写标签列表；不校验，由 dashboardTagError 判定。 */
export function parseDashboardTags(input: string): string[] {
  return input.split(/[,\s]+/).map((tag) => tag.trim().toLowerCase()).filter((tag) => tag !== "")
}

export function validDashboardTag(tag: string): boolean {
  return dashboardTagPattern.test(tag) && new TextEncoder().encode(tag).length <= maximumDashboardTagBytes
}

/** dashboardTagError 返回目录键，null 表示可以保存；规则与服务端 ValidateDashboard 一致。 */
export function dashboardTagError(tags: string[]): string | null {
  if (tags.length > maximumDashboardTags) return "workspace.dashboard_tags_too_many"
  const seen = new Set<string>()
  for (const tag of tags) {
    if (!validDashboardTag(tag)) return "workspace.dashboard_tag_invalid"
    if (seen.has(tag)) return "workspace.dashboard_tag_duplicate"
    seen.add(tag)
  }
  return null
}

/** dashboardMatchesQuery 只看标题与标签：空查询匹配全部，大小写不敏感。 */
export function dashboardMatchesQuery(definition: DashboardDefinition, query: string): boolean {
  const needle = query.trim().toLowerCase()
  if (needle === "") return true
  if (definition.title.toLowerCase().includes(needle)) return true
  return (definition.tags ?? []).some((tag) => tag.includes(needle))
}
