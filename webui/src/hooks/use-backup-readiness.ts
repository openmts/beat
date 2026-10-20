import { useCallback, useEffect, useState } from "react"

import { createBackup, listBackups } from "@/lib/backup-api"
import { latestReadyBackup } from "@/lib/metric-erasure-api"
import type { BackupRecord } from "@/types/backup"

export interface BackupReadiness {
  loading: boolean
  ready?: BackupRecord
  unknown: boolean
  creating: boolean
  create: () => Promise<void>
}

/**
 * 最近 24 小时的就绪备份是唯一可展示的恢复证据。读取失败必须按“未知”处理，
 * 不能把失败伪装成“没有备份”，也不能静默创建体积很大的备份。
 */
export function useBackupReadiness(open: boolean): BackupReadiness {
  const [state, setState] = useState<"idle" | "loading" | "known" | "unknown">("idle")
  const [ready, setReady] = useState<BackupRecord | undefined>(undefined)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    if (!open) return
    let active = true
    setState("loading")
    void listBackups().then(
      (page) => {
        if (!active) return
        setReady(latestReadyBackup(page.items, Date.now()))
        setState("known")
      },
      () => {
        if (!active) return
        setReady(undefined)
        setState("unknown")
      },
    )
    return () => {
      active = false
    }
  }, [open])

  const create = useCallback(async () => {
    setCreating(true)
    try {
      const record = await createBackup()
      setReady(record)
      setState("known")
    } finally {
      setCreating(false)
    }
  }, [])

  return {
    loading: state === "idle" || state === "loading",
    ready,
    unknown: state === "unknown",
    creating,
    create,
  }
}
