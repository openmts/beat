import { api } from "@/lib/api-client"
import type {
  BackupPage,
  BackupQuarantineInventory,
  BackupRecord,
  BackupState,
  BackupUploadChunk,
  BackupUploadDeclaration,
  BackupUploadSession,
} from "@/types/backup"

export interface BackupQuery {
  state?: BackupState
  cursor?: string
  limit?: number
}

// 备份历史只按不透明游标分页，服务端绝不整表下发。
export async function listBackups(query: BackupQuery = {}): Promise<BackupPage> {
  return (await api.get<BackupPage>("/admin/backups", { params: query })).data
}

// 隔离清单只暴露文件名、大小、原因与时间，用于确认孤儿归档数量。
export async function listBackupQuarantine(): Promise<BackupQuarantineInventory> {
  return (await api.get<BackupQuarantineInventory>("/admin/backups/quarantine")).data
}

export async function createBackup(): Promise<BackupRecord> {
  return (await api.post<BackupRecord>("/admin/backups")).data
}

export async function validateBackup(file: File): Promise<BackupRecord> {
  return (await api.post<BackupRecord>("/admin/backups/validate", file, {
    headers: { "Content-Type": "application/zip" },
  })).data
}

export async function deleteBackup(id: string): Promise<void> {
  await api.delete(`/admin/backups/${id}`)
}

export async function stageBackupRestore(id: string, confirmation: string): Promise<BackupRecord> {
  return (await api.post<BackupRecord>(`/admin/backups/${id}/stage-restore`, { confirmation })).data
}

export async function downloadBackup(record: BackupRecord): Promise<void> {
  const response = await api.get<Blob>(`/admin/backups/${record.id}/download`, { responseType: "blob" })
  const url = URL.createObjectURL(response.data)
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = record.filename
  anchor.click()
  URL.revokeObjectURL(url)
}

const uploadBase = "/admin/backups/uploads"

export async function startBackupUpload(declaration: BackupUploadDeclaration): Promise<BackupUploadSession> {
  return (await api.post<BackupUploadSession>(uploadBase, declaration)).data
}

export async function listBackupUploads(): Promise<BackupUploadSession[]> {
  return (await api.get<BackupUploadSession[]>(uploadBase)).data
}

export async function backupUploadStatus(id: string, signal?: AbortSignal): Promise<BackupUploadSession> {
  return (await api.get<BackupUploadSession>(`${uploadBase}/${encodeURIComponent(id)}/status`, { signal })).data
}

export async function uploadBackupChunk(id: string, chunk: {
  index: number
  start: number
  end: number
  total: number
  digest: string
  body: Uint8Array
  signal?: AbortSignal
}): Promise<BackupUploadChunk> {
  return (await api.put<BackupUploadChunk>(
    `${uploadBase}/${encodeURIComponent(id)}/chunks/${chunk.index}`,
    chunk.body,
    {
      signal: chunk.signal,
      headers: {
        "Content-Type": "application/octet-stream",
        "X-Beat-Chunk-Index": String(chunk.index),
        "Content-Range": `bytes ${chunk.start}-${chunk.end}/${chunk.total}`,
        "X-Content-SHA256": chunk.digest,
      },
    },
  )).data
}

export async function finalizeBackupUpload(id: string): Promise<BackupRecord> {
  return (await api.post<BackupRecord>(`${uploadBase}/${encodeURIComponent(id)}/finalize`)).data
}

export async function cancelBackupUpload(id: string): Promise<void> {
  await api.delete(`${uploadBase}/${encodeURIComponent(id)}`)
}
