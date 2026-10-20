import { createHash } from 'node:crypto'
import { gzipSync } from 'node:zlib'
import type { Plugin } from 'vite'
import { publicIcon } from './public-icons.ts'
import { publicWorker } from './public-worker.ts'
interface Chunk { type: 'chunk'; fileName: string; code: string; isEntry: boolean; facadeModuleId: string | null; imports: string[]; modules: Record<string, unknown>; viteMetadata?: { importedCss: Set<string>; importedAssets: Set<string> } }

// 公共外壳 gzip 预算：随随 zh-TW 目录发布由 250 KiB 调整到 256 KiB
// （owner 已批准本地化的公共外壳预算调整）；新增 ja/id 前需要拆分或再次评估。
const PUBLIC_JS_GZIP_BUDGET_KIB = 250
// 单分片上限按「谁会下载这个分片」分档：公共外壳与其语言分片是所有访客的首屏路径，
// 保持 500 KiB；管理端私有分片把全部语言目录内联进同一个入口，实测五语言 589 KiB，
// 因此按 600 KiB 约束——再加一门语言前必须先按语言拆分管理端目录（见
// docs/beat/localization-parity-design.md 的第十批进度）。
const PUBLIC_CHUNK_KIB = 500
const PRIVATE_CHUNK_KIB = 600
interface Asset { type: 'asset'; fileName: string; source: string | Uint8Array }
type Bundle = Record<string, Chunk | Asset>
function publicGraph(bundle: Bundle): Set<string> {
 const names = new Set<string>()
 const visit = (name: string) => {
  if (names.has(name)) return
  const file = bundle[name]; if (!file) throw new Error(`Public asset missing: ${name}`)
  names.add(name)
  if (file.type === 'chunk') {
   file.imports.forEach(visit)
   file.viteMetadata?.importedCss.forEach(visit); file.viteMetadata?.importedAssets.forEach(visit)
  } else if (name.endsWith('.css')) {
   for (const match of String(file.source).matchAll(/url\(["']?([^\s"')]+)["']?\)/g)) {
    if (match[1].startsWith('data:')) continue
    const asset = new URL(match[1], `https://build.invalid/${name}`).pathname.slice(1)
    if (bundle[asset]) visit(asset)
   }
  }
 }
 for (const file of Object.values(bundle)) if (file.type === 'chunk' && (file.isEntry || /\/pages\/dashboard\.tsx$/.test(file.facadeModuleId ?? ''))) visit(file.fileName)
 return names
}
// localeChunks 是公共外壳按语言懒加载的分片：它们不在静态公共图里，但每个访客都要下载一种，
// 因此预算按「公共图 + 最大的单个语言分片」计算，语言数量增加不会放宽任何访客的首屏上限。
function localeChunks(bundle: Bundle): Chunk[] {
 return Object.values(bundle).filter((file): file is Chunk =>
  file.type === 'chunk' && /\/context\/public-locales\/[^/]+\.tsx?$/.test(file.facadeModuleId ?? ''))
}
function checkPublicBudget(bundle: Bundle, names: Set<string>) {
 let gzipBytes = 0
 for (const name of names) {
  const file = bundle[name]
  if (file.type !== 'chunk') continue
  if (Object.keys(file.modules).some(id => /\/pages\/admin\/|\/pages\/admin\.tsx|\/node_modules\/(?:@xterm|recharts)\//.test(id))) throw new Error(`Private code in public graph: ${name}`)
  gzipBytes += gzipSync(file.code).byteLength
 }
 if (gzipBytes > PUBLIC_JS_GZIP_BUDGET_KIB * 1024) throw new Error(`Public JS exceeds ${PUBLIC_JS_GZIP_BUDGET_KIB} KiB gzip: ${gzipBytes}`)
 const localeGzip = localeChunks(bundle).reduce((max, chunk) => Math.max(max, gzipSync(chunk.code).byteLength), 0)
 if (gzipBytes + localeGzip > PUBLIC_JS_GZIP_BUDGET_KIB * 1024) throw new Error(`Public JS plus largest locale chunk exceeds ${PUBLIC_JS_GZIP_BUDGET_KIB} KiB gzip: ${gzipBytes} + ${localeGzip}`)
 // 每次构建打印实测预算，方便把数字直接记进门禁文档，避免手工复算。
 console.log(`[beat-public-shell] gzip 公共图 ${gzipBytes} B + 最大语言分片 ${localeGzip} B = ${gzipBytes + localeGzip} B / ${PUBLIC_JS_GZIP_BUDGET_KIB * 1024} B`)
 const publicFiles = new Set([...names, ...localeChunks(bundle).map(chunk => chunk.fileName)])
 for (const file of Object.values(bundle)) {
  if (file.type !== 'chunk') continue
  const limit = publicFiles.has(file.fileName) ? PUBLIC_CHUNK_KIB : PRIVATE_CHUNK_KIB
  const kib = Math.round(Buffer.byteLength(file.code) / 1024)
  if (kib > limit) throw new Error(`Chunk exceeds ${limit} KiB: ${file.fileName} (${kib} KiB)`)
 }
}
export function publicShellPlugin(): Plugin {
 const icons = [publicIcon(192), publicIcon(512)]
 return { name: 'beat-public-shell', apply: 'build', enforce: 'post',
  transformIndexHtml: { order: 'post', handler: html => html.replace(/<link[^>]+rel="icon"[^>]*>/, `<link rel="icon" type="image/png" href="/${icons[0].fileName}" />`).replace('</head>', '<link rel="manifest" href="/manifest.webmanifest" /><meta name="theme-color" content="#6d28d9" /></head>') },
  generateBundle: { order: 'post', handler(_options, output) {
   const bundle = output as unknown as Bundle
   const names = publicGraph(bundle); checkPublicBudget(bundle, names)
   // 语言分片随外壳一起预缓存，离线时也能保持当前语言；它们不计入静态公共图预算。
   for (const chunk of localeChunks(bundle)) names.add(chunk.fileName)
   const html = bundle['index.html']; if (!html || html.type !== 'asset') throw new Error('Missing public index HTML')
   const version = createHash('sha256').update([...names].sort().join('\n') + String(html.source)).digest('hex').slice(0, 16)
   const shell = `assets/public-shell-${version}.html`
   for (const icon of icons) { this.emitFile({ type: 'asset', ...icon }); names.add(icon.fileName) }
   this.emitFile({ type: 'asset', fileName: shell, source: html.source }); names.add(shell)
   const paths = [...names].sort().map(name => `/${name}`)
   this.emitFile({ type: 'asset', fileName: 'manifest.webmanifest', source: JSON.stringify({ id: '/', name: 'Beat', short_name: 'Beat', start_url: '/', scope: '/', display: 'standalone', dir: 'ltr', lang: 'en', background_color: '#ffffff', theme_color: '#6d28d9', icons: icons.flatMap(icon => ['any', 'maskable'].map(purpose => ({ src: `/${icon.fileName}`, sizes: icon.fileName.includes('-192-') ? '192x192' : '512x512', type: 'image/png', purpose }))) }) })
   this.emitFile({ type: 'asset', fileName: `assets/public-allowlist-${version}.json`, source: JSON.stringify({ version, paths }) })
   this.emitFile({ type: 'asset', fileName: 'sw.js', source: publicWorker(version, paths, `/${shell}`) })
  } },
 }
}
