import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api-client"
import {
  createBackup, deleteBackup, downloadBackup, listBackups, stageBackupRestore, validateBackup,
} from "@/lib/backup-api"
import type { BackupRecord } from "@/types/backup"

const record = { id: "backup", filename: "backup.zip", state: "ready" } as BackupRecord

describe("backup API", () => {
  let mock: MockAdapter

  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("maps backup lifecycle requests and downloads a blob", async () => {
    mock.onGet("/admin/backups").reply(200, [record])
    expect(await listBackups()).toEqual([record])
    mock.onPost("/admin/backups").reply(202, record)
    expect(await createBackup()).toEqual(record)

    const file = new File(["zip"], "backup.zip", { type: "application/zip" })
    mock.onPost("/admin/backups/validate").reply(201, { ...record, state: "validated" })
    expect((await validateBackup(file)).state).toBe("validated")
    mock.onDelete("/admin/backups/backup").reply(200)
    await deleteBackup("backup")
    mock.onPost("/admin/backups/backup/stage-restore").reply(200, { ...record, state: "staged" })
    expect((await stageBackupRestore("backup", "RESTORE BEAT")).state).toBe("staged")

    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:test")
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined)
    mock.onGet("/admin/backups/backup/download").reply(200, new Blob(["zip"]))
    await downloadBackup(record)
    expect(createObjectURL).toHaveBeenCalled()
    expect(click).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:test")
  })
})
