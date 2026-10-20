import { describe, expect, it, vi } from "vitest"
import {
  chunkSize, hashBackupFile, missingBackupChunks, resumableBackupUpload,
  type BackupUploadAPI,
} from "@/lib/backup-upload"
import type { BackupRecord, BackupUploadSession } from "@/types/backup"

const record: BackupRecord = {
  id: "backup-1", filename: "beat-upload-v1.zip", source: "uploaded", state: "validated",
  created_at: "2026-10-15T00:00:00Z", completed_at: null, snapshot_cutoff: null,
  size_bytes: 8, sqlite_bytes: 0, metrics_bytes: 0, metric_rows: 0, error_message: "",
  updated_at: "2026-10-15T00:00:00Z", reconciled_at: null,
}

function session(overrides: Partial<BackupUploadSession> = {}): BackupUploadSession {
  return {
    id: "session-1", owner_user_id: "owner", filename: "archive.zip", declared_bytes: 8,
    declared_digest: "0".repeat(64), chunk_bytes: 4, chunk_count: 2, received_bytes: 0,
    received_chunks: 0, state: "open", expires_at: "2026-10-16T00:00:00Z",
    created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:00:00Z",
    maximum_parallel_chunks: 2, received_indexes: [], ...overrides,
  }
}

function archive(): File {
  return new File([new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8])], "archive.zip",
    { type: "application/zip" })
}

function api(overrides: Partial<BackupUploadAPI> = {}): BackupUploadAPI {
  return {
    start: vi.fn().mockResolvedValue(session()),
    status: vi.fn().mockResolvedValue(session()),
    chunk: vi.fn().mockImplementation(async (_id, chunk) => ({
      session_id: "session-1", index: chunk.index, offset: chunk.start,
      size_bytes: chunk.end - chunk.start + 1, digest: chunk.digest,
      created_at: "2026-10-15T00:00:00Z",
    })),
    finalize: vi.fn().mockResolvedValue(record),
    ...overrides,
  }
}

describe("backup upload helpers", () => {
  it("hashes a blob in bounded slices and reports progress", async () => {
    const progress: number[] = []
    const digest = await hashBackupFile(archive(), { onProgress: (bytes) => progress.push(bytes) })
    expect(digest).toMatch(/^[0-9a-f]{64}$/)
    expect(progress).toEqual([8])
  })

  it("lists the chunks that still have to be uploaded", () => {
    expect(missingBackupChunks(session())).toEqual([0, 1])
    expect(missingBackupChunks(session({ received_indexes: [1] }))).toEqual([0])
    expect(chunkSize(session({ declared_bytes: 10, chunk_bytes: 4 }), 2)).toBe(2)
  })

  it("uploads missing chunks with bounded concurrency and finalizes", async () => {
    const upload = api()
    let concurrent = 0
    let peak = 0
    upload.chunk = vi.fn().mockImplementation(async (_id, chunk) => {
      concurrent += 1
      peak = Math.max(peak, concurrent)
      await new Promise((resolve) => window.setTimeout(resolve, 1))
      concurrent -= 1
      return { session_id: "session-1", index: chunk.index, offset: chunk.start,
        size_bytes: chunk.end - chunk.start + 1, digest: chunk.digest,
        created_at: "2026-10-15T00:00:00Z" }
    })
    const progress: string[] = []
    const result = await resumableBackupUpload(archive(), {
      api: upload, onProgress: (value) => progress.push(value.phase),
    })
    expect(result).toEqual(record)
    expect(upload.chunk).toHaveBeenCalledTimes(2)
    expect(peak).toBe(2)
    expect(upload.finalize).toHaveBeenCalledWith("session-1")
    expect(progress).toContain("hashing")
    expect(progress).toContain("uploading")
    expect(progress).toContain("finalizing")
  })

  it("resumes an unfinished session without restarting it", async () => {
    const digest = await hashBackupFile(archive())
    const status = vi.fn().mockResolvedValue(session({ declared_digest: digest,
      received_bytes: 4, received_chunks: 1, received_indexes: [0] }))
    const upload = api({ status })
    const onSession = vi.fn()
    await resumableBackupUpload(archive(), {
      api: upload, resumeSessionId: "session-1", onSession,
    })
    expect(upload.start).not.toHaveBeenCalled()
    expect(upload.chunk).toHaveBeenCalledTimes(1)
    expect(onSession).toHaveBeenCalledWith(expect.objectContaining({ id: "session-1" }))
  })

  it("retries transient chunk failures with jitter before succeeding", async () => {
    const chunk = vi.fn()
      .mockRejectedValueOnce(new Error("network"))
      .mockRejectedValueOnce(new Error("network"))
      .mockImplementation(async (_id, value) => ({ session_id: "session-1",
        index: value.index, offset: value.start, size_bytes: value.end - value.start + 1,
        digest: value.digest, created_at: "2026-10-15T00:00:00Z" }))
    const delay = vi.fn().mockResolvedValue(undefined)
    await resumableBackupUpload(archive(), {
      api: api({ chunk }), retries: 3, random: () => 0.5, delay,
    })
    expect(chunk).toHaveBeenCalledTimes(4)
    expect(delay).toHaveBeenCalledTimes(2)
    expect(delay.mock.calls[0][0]).toBe(250)
  })

  it("rejects a selection that does not match the resumable session", async () => {
    const status = vi.fn().mockResolvedValue(session({ filename: "other.zip" }))
    await expect(resumableBackupUpload(archive(), {
      api: api({ status }), resumeSessionId: "session-1",
    })).rejects.toThrow("does not match")
  })

  it("stops uploading once the signal is aborted", async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(resumableBackupUpload(archive(), {
      api: api(), signal: controller.signal,
    })).rejects.toBeDefined()
  })
})
