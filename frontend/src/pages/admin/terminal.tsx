import { useState, useEffect, useRef } from "react"
import { useNodes } from "@/hooks/use-api"
import { useLocale } from "@/context/locale"
import { executeBatchCommand } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Terminal as TerminalIcon, PlayIcon, SquareIcon } from "lucide-react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import "@xterm/xterm/css/xterm.css"
import { PageHeader } from "@/components/page-header"

function TerminalPage() {
  const { data: nodes, loading, error } = useNodes()
  const { t } = useLocale()
  const [selectedNodeId, setSelectedNodeId] = useState<string>("")
  const [connected, setConnected] = useState(false)
  const [batchCommand, setBatchCommand] = useState("")
  const [batchOutput, setBatchOutput] = useState("")
  const [batchRunning, setBatchRunning] = useState(false)
  const terminalRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    const handleResize = () => {
      fitRef.current?.fit()
    }
    window.addEventListener("resize", handleResize)
    return () => {
      window.removeEventListener("resize", handleResize)
      wsRef.current?.close()
      termRef.current?.dispose()
    }
  }, [])

  const handleConnect = () => {
    if (!selectedNodeId || !terminalRef.current) return

    if (termRef.current) {
      termRef.current.dispose()
    }

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
      theme: {
        background: "#1e1e2e",
        foreground: "#cdd6f4",
      },
    })

    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(terminalRef.current)
    fitAddon.fit()
    fitRef.current = fitAddon

    const protocol = window.location.protocol === "https:" ? "wss" : "ws"
    const wsUrl = `${protocol}://${window.location.host}/api/v1/ws/terminal?node_id=${encodeURIComponent(selectedNodeId)}`

    const _ws = new WebSocket(wsUrl)
    wsRef.current = _ws

    _ws.onopen = () => {
      setConnected(true)
    }

    _ws.onmessage = (event) => {
      term.write(event.data)
    }

    _ws.onclose = () => {
      setConnected(false)
      term.write(`\r\n\n[${t("terminal.closed")}]\r\n`)
    }

    _ws.onerror = () => {
      term.write(`\r\n\n[${t("terminal.failed")}]\r\n`)
      setConnected(false)
    }

    term.onData((data) => {
      if (_ws.readyState === WebSocket.OPEN) {
        _ws.send(data)
      }
    })

    termRef.current = term
  }

  const handleDisconnect = () => {
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    setConnected(false)
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

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("terminal.title")} description={t("terminal.description")} />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between py-3">
              <CardTitle className="text-base">{t("terminal.ssh")}</CardTitle>
              <div className="flex items-center gap-2">
                <Badge variant={connected ? "default" : "secondary"}>
                  {connected ? t("terminal.connected") : t("terminal.disconnected")}
                </Badge>
                {connected ? (
                  <Button variant="destructive" size="sm" onClick={handleDisconnect}>
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
            </CardHeader>
            <CardContent>
              {loading ? (
                <Skeleton className="h-80 w-full" />
              ) : (
                <div
                  ref={terminalRef}
                  className="h-80 w-full overflow-hidden rounded bg-[#1e1e2e]"
                />
              )}
            </CardContent>
          </Card>
        </div>

        <div className="flex flex-col gap-4">
          <Card>
            <CardHeader className="py-3">
              <CardTitle className="text-base">{t("terminal.select_node")}</CardTitle>
            </CardHeader>
            <CardContent>
              {loading ? (
                <Skeleton className="h-10 w-full" />
              ) : (
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
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="py-3">
              <CardTitle className="text-base">{t("terminal.batch")}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex flex-col gap-2">
                <Label>{t("terminal.command")}</Label>
                <Input
                  value={batchCommand}
                  onChange={(e) => setBatchCommand(e.target.value)}
                  placeholder="e.g. uptime"
                />
              </div>
              <Button
                onClick={handleBatchExecute}
                disabled={batchRunning || !batchCommand.trim() || onlineNodes.length === 0}
                className="w-full"
              >
                <PlayIcon data-icon="inline-start" />
                {t("terminal.execute_all")}
              </Button>
              {!loading && onlineNodes.length === 0 && (
                <p className="text-sm text-muted-foreground">{t("terminal.no_online_nodes")}</p>
              )}
              {batchOutput && (
                <pre className="max-h-60 overflow-auto rounded bg-muted p-3 font-mono text-xs">
                  {batchOutput}
                </pre>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

export default TerminalPage
