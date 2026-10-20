import { useEffect } from "react"
import { useLocation } from "react-router"
import { useSiteSettings } from "@/context/site-settings"
import { publicSurfaceForPath } from "@/lib/public-telemetry"

// PublicTelemetryTracker 只在策略开启时按粗粒度页面种类记录一次页面浏览。
export function PublicTelemetryTracker() {
  const location = useLocation()
  const { reportInteraction } = useSiteSettings()
  useEffect(() => {
    const surface = publicSurfaceForPath(location.pathname)
    if (surface) reportInteraction("page_view", surface)
  }, [location.pathname, reportInteraction])
  return null
}
