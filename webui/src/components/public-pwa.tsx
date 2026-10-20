import { useEffect, useEffectEvent, useRef, useState } from "react"
import { useLocation } from "react-router"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
interface InstallEvent extends Event { prompt: () => Promise<void>; userChoice: Promise<{ outcome: string }> }
import { publicIdle } from "@/lib/public-pwa-state"
export function PublicPWA() {
 const { pathname } = useLocation(), { t } = useLocale()
 const [waiting, setWaiting] = useState<ServiceWorker | null>(null), [install, setInstall] = useState<InstallEvent | null>(null)
 const [version, setVersion] = useState("")
 const [dismissed, setDismissed] = useState(false), [idle, setIdle] = useState(false), [message, setMessage] = useState("")
 const accepted = useRef(false), dismissedInstall = useRef(false)
 // 文案函数来自 context，测试替身每次渲染都会换身份；用 effect event 让 effect 依赖保持稳定。
 const text = useEffectEvent((key: string) => t(key))
 const isPublic = pathname === "/"
 useEffect(() => {
  if (!import.meta.env.PROD || !isPublic || !window.isSecureContext || !("serviceWorker" in navigator)) return
  let stopped = false, worker: ServiceWorker | null = null
  const updated = () => { if (!stopped && worker?.state === "installed" && navigator.serviceWorker.controller) setWaiting(worker) }
  const registered = (registration: ServiceWorkerRegistration) => { if (stopped) return; setWaiting(registration.waiting); registration.onupdatefound = () => { worker = registration.installing; worker?.addEventListener("statechange", updated) } }
  void navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" }).then(registered).catch(() => { if (!stopped) setMessage(text("shell.offline_unavailable")) })
  return () => { stopped = true; worker?.removeEventListener("statechange", updated) }
 }, [isPublic])
 useEffect(() => {
  const offered = (event: Event) => { event.preventDefault(); if (!dismissedInstall.current) setInstall(event as InstallEvent) }
  const state = () => setIdle(publicIdle())
  window.addEventListener("beforeinstallprompt", offered); document.addEventListener("focusin", state); document.addEventListener("focusout", state)
  const timer = window.setInterval(state, 1000); state()
  return () => { window.removeEventListener("beforeinstallprompt", offered); document.removeEventListener("focusin", state); document.removeEventListener("focusout", state); window.clearInterval(timer) }
 }, [])
 useEffect(() => {
  if (!("serviceWorker" in navigator)) return
  const receive = (event: MessageEvent) => {
   if (event.data?.type === "BEAT_VERSION" && typeof event.data.version === "string") setVersion(event.data.version.slice(0, 32))
   if (event.data?.type === "BEAT_UPDATE_DEFERRED") { accepted.current = false; setMessage(text("shell.update_deferred")) }
  }
  navigator.serviceWorker.addEventListener("message", receive); waiting?.postMessage({ type: "BEAT_GET_VERSION" })
  const changed = () => { if (accepted.current && waiting?.state === "activated") window.location.reload() }
  waiting?.addEventListener("statechange", changed)
  return () => { navigator.serviceWorker.removeEventListener("message", receive); waiting?.removeEventListener("statechange", changed) }
 }, [waiting])
 if (!isPublic) return null
 const update = () => { accepted.current = true; waiting?.postMessage({ type: "BEAT_ACCEPT_UPDATE" }) }
 const installApp = async () => {
  if (!install) return
  try { await install.prompt(); const choice = await install.userChoice; dismissedInstall.current = choice.outcome === "dismissed"; setInstall(null) }
  catch (error) { setMessage(error instanceof Error ? error.message : "Install unavailable"); setInstall(null) }
 }
 return <aside data-public-pwa className="fixed bottom-3 right-3 z-50 max-w-sm space-y-2 text-sm">
  {message && <div role="status" className="rounded-md border bg-background p-3">{message}<Button size="sm" variant="ghost" onClick={() => setMessage("")}>{t("shell.dismiss")}</Button></div>}
  {waiting && !dismissed && idle && <div role="status" className="rounded-md border bg-background p-3 shadow-sm"><p>{t("shell.version_ready")}{version && ` (${version})`}</p><Button size="sm" onClick={update}>{t("shell.update_app")}</Button><Button size="sm" variant="ghost" onClick={() => setDismissed(true)}>{t("shell.later")}</Button></div>}
  {install && <details className="rounded-md border bg-background p-2"><summary>{t("shell.app_options")}</summary><Button size="sm" variant="ghost" onClick={() => void installApp()}>{t("shell.install_app")}</Button><Button size="sm" variant="ghost" onClick={() => { dismissedInstall.current = true; setInstall(null) }}>{t("shell.dismiss")}</Button></details>}
 </aside>
}
