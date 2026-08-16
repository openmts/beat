import { useState, useEffect, useRef } from "react"
import { useNodes } from "@/hooks/use-api"
import { useLocale } from "@/context/locale"
import { useTheme } from "@/context/theme"
import { executeBatchCommand } from "@/lib/api"
import { darkTerminalTheme, lightTerminalTheme, type TerminalTheme } from "@/lib/terminal-theme"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Terminal as TerminalIcon, PlayIcon, SquareIcon, ServerIcon, GlobeIcon, ClockIcon, CopyIcon, CheckIcon, ShieldCheckIcon } from "lucide-react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { WebLinksAddon } from "@xterm/addon-web-links"
import "@xterm/xterm/css/xterm.css"
import { PageHeader } from "@/components/page-header"

function TerminalPage() {
  const { data: nodes, loading, error } = useNodes()
  const { t } = useLocale()
  const { theme } = useTheme()
  const [selectedNodeId, setSelectedNodeId] = useState<string>("")
  const [connected, setConnected] = useState(false)
  const [connectedAt, setConnectedAt] = useState<number | null>(null)
  const [now, setNow] = useState(Date.now())
  const [batchCommand, setBatchCommand] = useState("")
  const [batchOutput, setBatchOutput] = useState("")
  const [batchRunning, setBatchRunning] = useState(false)
  const [copiedCommand, setCopiedCommand] = useState(false)
  const terminalRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    const handleResize = () => {
      fitRef.current?.fit()
    }
    window.addEventListener("resize", handleResize)
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => {
      window.removeEventListener("resize", handleResize)
      window.clearInterval(timer)
      wsRef.current?.close()
      termRef.current?.dispose()
    }
  }, [])

  const terminalTheme: TerminalTheme = theme === "light" ? lightTerminalTheme : darkTerminalTheme

  // xterm's default stylesheet paints the terminal black; pin the background and
  // text color to the active palette so mobile browsers (which may not support
  // oklch) never render an unreadable shell.
  useEffect(() => {
    if (terminalRef.current) {
      applyTerminalColors(terminalRef.current, terminalTheme)
    }
  }, [terminalTheme])

  const handleConnect = () => {
    if (!selectedNodeId || !terminalRef.current) return

    if (termRef.current) {
      termRef.current.dispose()
    }

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      theme: terminalTheme,
    })

    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.loadAddon(new WebLinksAddon())
    term.open(terminalRef.current)
    applyTerminalColors(terminalRef.current, terminalTheme)
    fitAddon.fit()
    fitRef.current = fitAddon

    const protocol = window.location.protocol === "https:" ? "wss" : "ws"
    const wsUrl = `${protocol}://${window.location.host}/api/v1/ws/terminal?node_id=${encodeURIComponent(selectedNodeId)}`

    const _ws = new WebSocket(wsUrl)
    wsRef.current = _ws

    _ws.onopen = () => {
      setConnected(true)
      setConnectedAt(Date.now())
    }

    _ws.onmessage = (event) => {
      term.write(event.data)
    }

    _ws.onclose = () => {
      setConnected(false)
      setConnectedAt(null)
      term.write(`\r\n\n[${t("terminal.closed")}]\r\n`)
    }

    _ws.onerror = () => {
      term.write(`\r\n\n[${t("terminal.failed")}]\r\n`)
      setConnected(false)
      setConnectedAt(null)
    }

    term.onData((data) => {
      if (_ws.readyState === WebSocket.OPEN) {
        _ws.send(data)
      }
    })

    termRef.current = term
  }

  // Keep an open terminal in sync when the app theme switches.
  useEffect(() => {
    if (termRef.current) {
      termRef.current.options.theme = terminalTheme
      fitRef.current?.fit()
    }
  }, [terminalTheme])

  const handleDisconnect = () => {
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    setConnected(false)
    setConnectedAt(null)
  }

  const handleCopyCommand = async () => {
    if (!batchCommand.trim()) return
    try {
      await navigator.clipboard.writeText(batchCommand.trim())
      setCopiedCommand(true)
      window.setTimeout(() => setCopiedCommand(false), 1500)
    } catch {
      setCopiedCommand(false)
    }
  }

  const handleBatchExecute = async () => {
    if (!batchCommand.trim()) return
    setBatchRunning(true)
    setBatchOutput("")
    try {
      const onlineNodes = nodes?.filter((n) => n.status === "online") ?? []
      const response = await executeBatchCommand(onlineNodes.map((node) => node.id), batchCommand)
      setBatchOutput(response.map((result) => {
        const heading = `--- ${result.node_name || result.node_id} ---`
        return [heading, result.error ? `${t("terminal.batch_failed")}: ${result.error}` : result.output || ""].join("\n")
      }).join("\n\n"))
    } catch (err) {
      setBatchOutput(err instanceof Error ? err.message : t("app.error"))
    } finally {
      setBatchRunning(false)
    }
  }

  const onlineNodes = nodes?.filter((n) => n.status === "online") ?? []
  const nodeOptions = onlineNodes.map((node) => ({
    label: `${node.alias || node.name} (${node.host})`,
    value: node.id,
  }))
  const selectedNode = nodes?.find((n) => n.id === selectedNodeId) ?? null
  const connectedDuration = connected && connectedAt
    ? formatDuration(Math.max(0, Math.floor((now - connectedAt) / 1000)))
    : null

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader title={t("terminal.title")} description={t("terminal.description")} />

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
        {/* Terminal window */}
        <div className="lg:col-span-2">
          <div className="overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10 shadow-sm">
            {/* Title bar */}
            <div className="flex items-center gap-3 border-b border-foreground/10 bg-muted/40 px-4 py-2.5">
              <div className="flex shrink-0 items-center gap-1.5" aria-hidden>
                <span className="size-3 rounded-full bg-[#ff5f57]" />
                <span className="size-3 rounded-full bg-[#febc2e]" />
                <span className="size-3 rounded-full bg-[#28c840]" />
              </div>
              <div className="flex min-w-0 items-center gap-2 text-xs font-medium text-foreground">
                <TerminalIcon className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate">
                  {selectedNode ? selectedNode.alias || selectedNode.name : t("terminal.select_placeholder")}
                </span>
                {selectedNode && (
                  <span className="hidden items-center gap-1 text-muted-foreground sm:flex">
                    <GlobeIcon className="size-3" />
                    <span className="truncate">{selectedNode.host}</span>
                  </span>
                )}
              </div>
              <div className="ml-auto flex items-center gap-2">
                {connected && (
                  <span className="hidden items-center gap-1.5 rounded-full bg-success/10 px-2.5 py-1 text-[11px] font-medium text-success sm:flex">
                    <span className="size-1.5 rounded-full bg-success animate-pulse" />
                    {t("terminal.connected")}
                    {connectedDuration && <span className="tabular-nums">{connectedDuration}</span>}
                  </span>
                )}
                {connected ? (
                  <Button variant="outline" size="sm" onClick={handleDisconnect}>
                    <SquareIcon data-icon="inline-start" />
                    {t("terminal.disconnect")}
                  </Button>
                ) : (
                  <Button size="sm" onClick={handleConnect} disabled={!selectedNodeId}>
                    <TerminalIcon data-icon="inline-start" />
                    {t("terminal.connect")}
                  </Button>
                )}
              </div>
            </div>

            {/* Terminal body */}
            <div className="p-2 sm:p-3">
              {loading ? (
                <Skeleton className="h-80 w-full" />
              ) : (
                <div className="relative">
                  <div
                    ref={terminalRef}
                    className="h-80 w-full overflow-hidden rounded-lg border border-foreground/10 bg-background font-mono shadow-inner focus-within:ring-2 focus-within:ring-ring/50"
                  />
                  {!selectedNodeId && !loading && (
                    <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 text-muted-foreground">
                      <ServerIcon className="size-8" />
                      <p className="text-sm">{t("terminal.no_node_selected")}</p>
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Side panel */}
        <div className="flex flex-col gap-5">
          <Card>
            <CardHeader className="py-3">
              <CardTitle className="flex items-center gap-2 text-base">
                <ServerIcon className="size-4 text-muted-foreground" />
                {t("terminal.select_node")}
              </CardTitle>
            </CardHeader>
            <CardContent>
              {loading ? (
                <Skeleton className="h-10 w-full" />
              ) : (
                <div className="flex flex-col gap-2.5">
                  <Select
                    items={nodeOptions}
                    value={selectedNodeId}
                    onValueChange={(v) => { if (v) setSelectedNodeId(v) }}
                    disabled={connected}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={t("terminal.select_placeholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {nodeOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  {selectedNode && !connected && (
                    <div className="flex items-center gap-2 rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
                      <ShieldCheckIcon className="size-3.5 shrink-0" />
                      <span className="truncate">
                        {t("terminal.ready_connect")} {selectedNode.host}
                      </span>
                    </div>
                  )}
                  {!selectedNodeId && (
                    <p className="text-xs text-muted-foreground">{t("terminal.no_online_nodes")}</p>
                  )}
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="py-3">
              <CardTitle className="flex items-center justify-between gap-2 text-base">
                <span className="flex items-center gap-2">
                  <PlayIcon className="size-4 text-muted-foreground" />
                  {t("terminal.batch")}
                </span>
                <Badge variant="secondary">{onlineNodes.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex flex-col gap-2">
                <Label>{t("terminal.command")}</Label>
                <div className="flex gap-2">
                  <Input
                    value={batchCommand}
                    onChange={(e) => setBatchCommand(e.target.value)}
                    placeholder="e.g. uptime"
                    onKeyDown={(e) => { if (e.key === "Enter") handleBatchExecute() }}
                  />
                  <Button
                    variant="outline"
                    size="icon"
                    onClick={handleCopyCommand}
                    disabled={!batchCommand.trim()}
                    aria-label={t("terminal.copy")}
                  >
                    {copiedCommand ? <CheckIcon data-icon /> : <CopyIcon data-icon />}
                  </Button>
                </div>
              </div>
              <Button
                onClick={handleBatchExecute}
                disabled={batchRunning || !batchCommand.trim() || onlineNodes.length === 0}
                className="w-full"
              >
                {batchRunning ? (
                  <span className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                ) : (
                  <PlayIcon data-icon="inline-start" />
                )}
                {t("terminal.execute_all")}
              </Button>
              {batchOutput && (
                <div className="overflow-hidden rounded-lg border border-foreground/10">
                  <div className="flex items-center justify-between border-b border-foreground/10 bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground">
                    <span>{t("terminal.output")}</span>
                    <ClockIcon className="size-3.5" />
                  </div>
                  <pre className="max-h-64 overflow-auto bg-muted/20 p-3 font-mono text-xs leading-relaxed">
                    {batchOutput}
                  </pre>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

export default TerminalPage

function applyTerminalColors(container: HTMLElement, theme: TerminalTheme) {
  const xterm = container.querySelector<HTMLElement>(".xterm")
  if (xterm) {
    xterm.style.backgroundColor = theme.background
    xterm.style.background = theme.background
  }
  container.style.backgroundColor = theme.background
}

function formatDuration(totalSeconds: number): string {
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`
  return `${minutes}:${String(seconds).padStart(2, "0")}`
}
