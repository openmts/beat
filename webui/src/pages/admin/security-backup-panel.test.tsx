import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityBackupPanel } from "@/pages/admin/security-backup-panel"
import * as backupAPI from "@/lib/backup-api"
import type { BackupRecord } from "@/types/backup"

vi.mock("@/lib/backup-api")

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
}

describe("security backup panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(backupAPI.listBackups).mockResolvedValue([readyBackup])
    vi.mocked(backupAPI.createBackup).mockResolvedValue({ ...readyBackup, id: "new", state: "running" })
    vi.mocked(backupAPI.validateBackup).mockResolvedValue({ ...readyBackup, id: "upload", state: "validated" })
    vi.mocked(backupAPI.downloadBackup).mockResolvedValue()
    vi.mocked(backupAPI.deleteBackup).mockResolvedValue()
    vi.mocked(backupAPI.stageBackupRestore).mockResolvedValue({ ...readyBackup, state: "staged" })
  })

  it("creates, uploads, downloads, and deletes archives", async () => {
    const { container } = render(<LocaleProvider><SecurityBackupPanel /></LocaleProvider>)
    await screen.findByText(readyBackup.filename)

    fireEvent.click(screen.getByRole("button", { name: "Create backup" }))
    await waitFor(() => expect(backupAPI.createBackup).toHaveBeenCalled())

    const file = new File(["zip"], "backup.zip", { type: "application/zip" })
    const input = container.querySelector<HTMLInputElement>('input[type="file"]')
    expect(input).not.toBeNull()
    fireEvent.change(input!, { target: { files: [file] } })
    await waitFor(() => expect(backupAPI.validateBackup).toHaveBeenCalledWith(file))

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
})
