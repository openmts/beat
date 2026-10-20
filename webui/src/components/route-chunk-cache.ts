import { lazy, type ComponentType, type LazyExoticComponent } from "react"
export type ChunkLoader<P = Record<string, never>> = () => Promise<{ default: ComponentType<P> }>
export type RouteLoader = ChunkLoader
export type RouteChunk<P = Record<string, never>> = { Page: LazyExoticComponent<ComponentType<P>>; key: number }
// loader 都是模块级常量，这里用强引用缓存；内联 loader 按源码文本建条目，不会随渲染增长。
const cache = new Map<unknown, unknown>()
let nextChunkKey = 0
// 分片按模块复用：内联 loader（`load={() => import("@/pages/x")}`）每次渲染都是新函数，
// transition 重试会用新身份重建 lazy 并反复挂起，页面只能手动刷新恢复。
const inlineLoader = /^\(\s*\)\s*=>[^$+]*import[\w$]*\(\s*["']/
function cacheKey(load: unknown): unknown {
 const source = "" + load
 return inlineLoader.test(source) ? source : load
}
export function stableChunk<P = Record<string, never>>(load: ChunkLoader<P>): RouteChunk<P> {
 const key = cacheKey(load)
 const known = cache.get(key) as RouteChunk<P> | undefined
 if (known) return known
 const chunk = { Page: lazy(load), key: nextChunkKey++ }
 cache.set(key, chunk)
 return chunk
}
export function resetChunk(load: object): void { cache.delete(cacheKey(load)) }
