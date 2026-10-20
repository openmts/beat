import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityBackupPanel } from "@/pages/admin/security-backup-panel"
import * as backupAPI from "@/lib/backup-api"
import type { BackupRecord } from "@/types/backup"

vi.mock("@/lib/backup-api")
const auth = vi.hoisted(() => ({ principal: { user: { role: "owner", totp_enabled_at: null as string | null } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))

const readyBackup: BackupRecord = {
  id: "backup-1",
  filename: "beat-backup-v1.zip",
  source: "generated",
  state: "ready",
  created_at: "2026-07-30T00:00:00Z",
  completed_at: "2026-07-30T00:01:00Z",
  snapshot_cutoff: "2026-07-30T00:00:30Z",
  size_bytes: 2048,
  sqlite_bytes: 1024,
  metrics_bytes: 1024,
  metric_rows: 12,
  error_message: "",
  updated_at: "2026-07-30T00:01:00Z",
  reconciled_at: null,
}

describe("security backup panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(backupAPI.listBackups).mockResolvedValue({ items: [readyBackup] })
    vi.mocked(backupAPI.listBackupQuarantine).mockResolvedValue({ items: [], total: 0 })

    vi.mocked(backupAPI.createBackup).mockResolvedValue({ ...readyBackup, id: "new", state: "running" })
    vi.mocked(backupAPI.listBackupUploads).mockResolvedValue([])

    vi.mocked(backupAPI.startBackupUpload).mockResolvedValue({
      id: "upload-1", owner_user_id: "owner", filename: "backup.zip", declared_bytes: 3,
      declared_digest: "a".repeat(64), chunk_bytes: 1024, chunk_count: 1, received_bytes: 0,
      received_chunks: 0, state: "open", expires_at: "2026-07-31T00:00:00Z",
      created_at: "2026-07-30T00:00:00Z", updated_at: "2026-07-30T00:00:00Z",
      maximum_parallel_chunks: 4, received_indexes: [],
    })
    vi.mocked(backupAPI.uploadBackupChunk).mockResolvedValue({
      session_id: "upload-1", index: 0, offset: 0, size_bytes: 3, digest: "a".repeat(64),
      created_at: "2026-07-30T00:00:00Z",
    })
    vi.mocked(backupAPI.finalizeBackupUpload).mockResolvedValue({ ...readyBackup, id: "upload", state: "validated" })
    vi.mocked(backupAPI.cancelBackupUpload).mockResolvedValue()
    vi.mocked(backupAPI.downloadBackup).mockResolvedValue()
    vi.mocked(backupAPI.deleteBackup).mockResolvedValue()
    vi.mocked(backupAPI.stageBackupRestore).mockResolvedValue({ ...readyBackup, state: "staged" })
  })

  it("creates, uploads, downloads, and deletes archives", async () => {
    render(<LocaleProvider><SecurityBackupPanel /></LocaleProvider>)
    await screen.findByText(readyBackup.filename)

    fireEvent.click(screen.getByRole("button", { name: "Create backup" }))
    await waitFor(() => expect(backupAPI.createBackup).toHaveBeenCalled())

    fireEvent.click(screen.getByRole("button", { name: "Upload archive" }))
    const file = new File(["zip"], "backup.zip", { type: "application/zip" })
    const input = await screen.findByLabelText("Archive file")
    expect(input).toBeInstanceOf(HTMLInputElement)
    fireEvent.change(input, { target: { files: [file] } })
    fireEvent.click(await screen.findByRole("button", { name: "Upload and validate" }))
    await waitFor(() => expect(backupAPI.finalizeBackupUpload).toHaveBeenCalledWith("upload-1"))
    expect(backupAPI.startBackupUpload).toHaveBeenCalledWith({
      filename: "backup.zip", size_bytes: 3, digest: expect.stringMatching(/^[0-9a-f]{64}$/),
    })

    fireEvent.click(screen.getByRole("button", { name: "Download" }))
    await waitFor(() => expect(backupAPI.downloadBackup).toHaveBeenCalledWith(readyBackup))
    fireEvent.click(screen.getByRole("button", { name: "Delete backup" }))
    await waitFor(() => expect(backupAPI.deleteBackup).toHaveBeenCalledWith("backup-1"))
  })

  it("requires the exact phrase before staging a restore", async () => {
    render(<LocaleProvider><SecurityBackupPanel /></LocaleProvider>)
    await screen.findByText(readyBackup.filename)
    fireEvent.click(screen.getByRole("button", { name: "Restore" }))
    const stage = screen.getByRole("button", { name: "Apply on restart" })
    expect(stage).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Type RESTORE BEAT to confirm"), {
      target: { value: "RESTORE BEAT" },
    })
    expect(stage).toBeEnabled()
    fireEvent.click(stage)
    await waitFor(() => expect(backupAPI.stageBackupRestore).toHaveBeenCalledWith("backup-1", "RESTORE BEAT"))
  })

  it("reports quarantined orphan archives without exposing server paths", async () => {
    vi.mocked(backupAPI.listBackupQuarantine).mockResolvedValue({
      items: [{ id: "q", filename: "beat-orphan.zip", size_bytes: 4, reason: "orphan",
        quarantined_at: "2026-07-30T00:00:00Z" }],
      total: 1,
    })
    render(<LocaleProvider><SecurityBackupPanel /></LocaleProvider>)
    expect(await screen.findByText("Quarantined orphan archives: 1")).toBeInTheDocument()
  })
})
