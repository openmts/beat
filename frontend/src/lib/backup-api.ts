import { api } from "@/lib/api-client"
import type { BackupRecord } from "@/types/backup"

export async function listBackups(): Promise<BackupRecord[]> {
  return (await api.get<BackupRecord[]>("/admin/backups")).data
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
