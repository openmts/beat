import { deflateSync } from 'node:zlib'
import { createHash } from 'node:crypto'
function crc32(bytes: Uint8Array): number {
 let crc = 0xffffffff
 for (const byte of bytes) {
  crc ^= byte
  for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0)
 }
 return (crc ^ 0xffffffff) >>> 0
}
function chunk(name: string, data: Buffer): Buffer {
 const type = Buffer.from(name), length = Buffer.alloc(4), checksum = Buffer.alloc(4)
 length.writeUInt32BE(data.length); checksum.writeUInt32BE(crc32(Buffer.concat([type, data])))
 return Buffer.concat([length, type, data, checksum])
}
export function publicIcon(size: number): { fileName: string; source: Buffer } {
 const pixels = Buffer.alloc((size * 4 + 1) * size)
 for (let y = 0; y < size; y++) {
  for (let x = 0; x < size; x++) {
   const offset = y * (size * 4 + 1) + 1 + x * 4
   const relativeX = x / size, relativeY = y / size
   const mark = relativeX > 0.25 && relativeX < 0.75 && Math.abs(relativeY - (relativeX < 0.4 ? 0.5 : relativeX < 0.5 ? 0.3 : relativeX < 0.6 ? 0.7 : 0.5)) < 0.035
   pixels.set(mark ? [255, 255, 255, 255] : [109, 40, 217, 255], offset)
  }
 }
 const header = Buffer.alloc(13); header.writeUInt32BE(size); header.writeUInt32BE(size, 4); header[8] = 8; header[9] = 6
 const source = Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(pixels)), chunk('IEND', Buffer.alloc(0))])
 return { fileName: `assets/beat-${size}-${createHash('sha256').update(source).digest('hex').slice(0, 12)}.png`, source }
}
