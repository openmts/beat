/** 工作区里的指标读共享一个并发闸门。服务端按身份限制在途请求（管理读 4、公共历史 2），
 *  而看板加载会同时发起目录、看板、节点、分组、面板查询、注释与面板告警；不加以限制时，
 *  多面板看板会撞上准入拒绝，个别面板只能显示「request rejected」。
 *  这里把同一标签页里的这些读串行到两个并发：既保留并行加载，又给重试和范围切换留出余量。 */
export const workspaceReadConcurrency = 2

let active = 0
const waiting: Array<() => void> = []

/** acquireWorkspaceReadSlot 取号；返回的释放函数同时唤醒下一个排队者。 */
async function acquireWorkspaceReadSlot(): Promise<() => void> {
  if (active >= workspaceReadConcurrency) await new Promise<void>((resolve) => { waiting.push(resolve) })
  active += 1
  return () => {
    active -= 1
    waiting.shift()?.()
  }
}

/** withWorkspaceReadSlot 让一次指标读在闸门内执行；抛错与中止同样释放名额。 */
export async function withWorkspaceReadSlot<T>(run: () => Promise<T>): Promise<T> {
  const release = await acquireWorkspaceReadSlot()
  try {
    return await run()
  } finally {
    release()
  }
}
