import { sha256 } from "@noble/hashes/sha2.js"
import { bytesToHex } from "@noble/hashes/utils.js"
import {
  backupUploadStatus,
  finalizeBackupUpload,
  startBackupUpload,
  uploadBackupChunk,
} from "@/lib/backup-api"
import type {
  BackupRecord,
  BackupUploadChunk,
  BackupUploadDeclaration,
  BackupUploadSession,
} from "@/types/backup"

export const backupHashChunkBytes = 4 * 1024 * 1024
export const defaultUploadRetries = 3
export const maximumUploadRetryDelayMS = 8000

export interface BackupChunkUpload {
  index: number
  start: number
  end: number
  total: number
  digest: string
  body: Uint8Array
  signal?: AbortSignal
}

export interface BackupUploadAPI {
  start: (declaration: BackupUploadDeclaration) => Promise<BackupUploadSession>
  status: (id: string, signal?: AbortSignal) => Promise<BackupUploadSession>
  chunk: (id: string, chunk: BackupChunkUpload) => Promise<BackupUploadChunk>
  finalize: (id: string) => Promise<BackupRecord>
}

const liveAPI: BackupUploadAPI = {
  start: startBackupUpload,
  status: backupUploadStatus,
  chunk: uploadBackupChunk,
  finalize: finalizeBackupUpload,
}

export interface BackupUploadProgress {
  phase: "hashing" | "uploading" | "finalizing"
  uploadedBytes: number
  totalBytes: number
  completedChunks: number
  totalChunks: number
}

export interface BackupUploadOptions {
  signal?: AbortSignal
  onProgress?: (progress: BackupUploadProgress) => void
  onSession?: (session: BackupUploadSession) => void
  api?: BackupUploadAPI
  resumeSessionId?: string
  retries?: number
  random?: () => number
  delay?: (ms: number, signal?: AbortSignal) => Promise<void>
}

// hashBackupFile 分块计算整档 SHA-256，避免把大归档整体读入内存。
export async function hashBackupFile(
  file: Blob,
  options: { signal?: AbortSignal; onProgress?: (bytes: number) => void } = {},
): Promise<string> {
  const hash = sha256.create()
  try {
    for (let offset = 0; offset < file.size; offset += backupHashChunkBytes) {
      options.signal?.throwIfAborted()
      const end = Math.min(offset + backupHashChunkBytes, file.size)
      const bytes = new Uint8Array(await file.slice(offset, end).arrayBuffer())
      hash.update(bytes)
      bytes.fill(0)
      options.onProgress?.(end)
    }
    options.signal?.throwIfAborted()
    return bytesToHex(hash.digest())
  } finally {
    hash.destroy()
  }
}

// missingBackupChunks 返回尚未被服务端确认的分块索引。
export function missingBackupChunks(session: BackupUploadSession): number[] {
  const received = new Set(session.received_indexes ?? [])
  const missing: number[] = []
  for (let index = 0; index < session.chunk_count; index += 1) {
    if (!received.has(index)) missing.push(index)
  }
  return missing
}

export function chunkSize(session: BackupUploadSession, index: number): number {
  const start = index * session.chunk_bytes
  return Math.max(0, Math.min(session.chunk_bytes, session.declared_bytes - start))
}

// resumableBackupUpload 使用有界并发上传分块，失败重试带抖动，取消后仍可续传。
export async function resumableBackupUpload(
  file: File,
  options: BackupUploadOptions = {},
): Promise<BackupRecord> {
  const api = options.api ?? liveAPI
  const signal = options.signal
  const digest = await hashBackupFile(file, {
    signal,
    onProgress: (bytes) => report(options, {
      phase: "hashing", uploadedBytes: bytes, totalBytes: file.size,
      completedChunks: 0, totalChunks: 0,
    }),
  })
  const session = await resolveSession(api, options, file, digest)
  if (session.filename !== file.name || session.declared_bytes !== file.size) {
    throw new Error("The selected file does not match this upload session")
  }
  options.onSession?.(session)
  await uploadMissingChunks(api, session, file, options)
  report(options, {
    phase: "finalizing", uploadedBytes: file.size, totalBytes: file.size,
    completedChunks: session.chunk_count, totalChunks: session.chunk_count,
  })
  return api.finalize(session.id)
}

async function resolveSession(
  api: BackupUploadAPI,
  options: BackupUploadOptions,
  file: File,
  digest: string,
): Promise<BackupUploadSession> {
  if (options.resumeSessionId) {
    const session = await api.status(options.resumeSessionId, options.signal)
    if (session.state !== "open") throw new Error("The upload session is no longer open")
    if (session.declared_bytes !== file.size || session.declared_digest !== digest) {
      throw new Error("The selected file does not match this upload session")
    }
    return session
  }
  return api.start({ filename: file.name, size_bytes: file.size, digest })
}

async function uploadMissingChunks(
  api: BackupUploadAPI,
  session: BackupUploadSession,
  file: File,
  options: BackupUploadOptions,
): Promise<void> {
  const missing = missingBackupChunks(session)
  const state = { completed: session.chunk_count - missing.length, running: 0 }
  report(options, {
    phase: "uploading", uploadedBytes: receivedBytes(session, file),
    totalBytes: file.size, completedChunks: state.completed, totalChunks: session.chunk_count,
  })
  const concurrency = Math.max(1, Math.min(session.maximum_parallel_chunks || 1, missing.length || 1))
  const queue = [...missing]
  const worker = async () => {
    for (;;) {
      options.signal?.throwIfAborted()
      const index = queue.shift()
      if (index === undefined) return
      await uploadSingleChunk(api, session, file, index, options)
      state.completed += 1
      report(options, {
        phase: "uploading", uploadedBytes: receivedBytes(session, file, state.completed - missing.length),
        totalBytes: file.size, completedChunks: state.completed, totalChunks: session.chunk_count,
      })
    }
  }
  await Promise.all(Array.from({ length: concurrency }, worker))
}

function receivedBytes(session: BackupUploadSession, file: File, extra = 0): number {
  return Math.min(file.size, session.received_bytes + extra)
}

async function uploadSingleChunk(
  api: BackupUploadAPI,
  session: BackupUploadSession,
  file: File,
  index: number,
  options: BackupUploadOptions,
): Promise<void> {
  const start = index * session.chunk_bytes
  const end = Math.min(start + session.chunk_bytes, session.declared_bytes)
  const bytes = new Uint8Array(await file.slice(start, end).arrayBuffer())
  const digest = bytesToHex(sha256(bytes))
  try {
    const retries = options.retries ?? defaultUploadRetries
    await withRetry(() => api.chunk(session.id, {
      index, start, end: end - 1, total: session.declared_bytes, digest, body: bytes,
      signal: options.signal,
    }), { ...options, retries })
  } finally {
    bytes.fill(0)
  }
}

async function withRetry<T>(
  operation: () => Promise<T>,
  options: BackupUploadOptions & { retries: number },
): Promise<T> {
  const random = options.random ?? Math.random
  const delay = options.delay ?? waitForAbort
  for (let attempt = 0; ; attempt += 1) {
    options.signal?.throwIfAborted()
    try {
      return await operation()
    } catch (cause) {
      options.signal?.throwIfAborted()
      if (attempt >= options.retries) throw cause
      const backoff = Math.min(maximumUploadRetryDelayMS, 250 * 2 ** attempt)
      await delay(Math.round(backoff * (0.5 + random())), options.signal)
    }
  }
}

function waitForAbort(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) { reject(signal.reason); return }
    const abort = () => { window.clearTimeout(timer); reject(signal?.reason) }
    const timer = window.setTimeout(() => { signal?.removeEventListener("abort", abort); resolve() }, ms)
    signal?.addEventListener("abort", abort, { once: true })
  })
}

function report(options: BackupUploadOptions, progress: BackupUploadProgress): void {
  options.onProgress?.(progress)
}
