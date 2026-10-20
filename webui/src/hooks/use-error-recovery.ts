import { useEffect, useRef } from "react"

// 服务端升级窗口内进入错误态的页面不会自己重试：用户切回标签页、网络恢复或从前进
// 后退缓存返回时再试一次，避免刷新前一直停在错误提示上。
export function useErrorRecovery(active: boolean, recover: () => void): void {
  const latest = useRef(recover)
  latest.current = recover
  useEffect(() => {
    if (!active) return
    const attempt = () => {
      if (document.hidden !== true) latest.current()
    }
    window.addEventListener("online", attempt)
    window.addEventListener("pageshow", attempt)
    document.addEventListener("visibilitychange", attempt)
    return () => {
      window.removeEventListener("online", attempt)
      window.removeEventListener("pageshow", attempt)
      document.removeEventListener("visibilitychange", attempt)
    }
  }, [active])
}
