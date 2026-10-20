import { useCallback, useEffect, useState } from "react"
import { listEnrollmentGrants } from "@/lib/enrollment-discovery-api"
import { APIError } from "@/lib/api-error"
import type { EnrollmentGrant } from "@/types/enrollment"

interface GrantState {
  data: EnrollmentGrant[] | null
  loading: boolean
  error: string
  // 0 表示非 HTTP 失败；428 表示必须先完成近期重认证才能读取授权。
  status: number
}

const emptyState: GrantState = { data: null, loading: false, error: "", status: 0 }

/** 授权列表只有所有者可读；非所有者直接返回空状态且不发起请求。 */
export function useEnrollmentGrants(enabled: boolean) {
  const [state, setState] = useState<GrantState>(emptyState)
  const refresh = useCallback(async (options?: { silent?: boolean }) => {
    if (!enabled) {
      setState(emptyState)
      return
    }
    setState((current) => ({ ...current, loading: options?.silent !== true, error: "", status: 0 }))
    try {
      const grants = await listEnrollmentGrants()
      setState({ data: grants, loading: false, error: "", status: 0 })
    } catch (failure) {
      setState((current) => ({ ...current, loading: false,
        error: failure instanceof Error ? failure.message : "Unknown error",
        status: failure instanceof APIError ? failure.status : 0 }))
    }
  }, [enabled])
  useEffect(() => { void refresh() }, [refresh])
  return { ...state, refresh }
}
