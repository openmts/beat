export type AgentArchitecture = "amd64" | "arm64"

export const agentArchitectures = [
  { label: "Linux · x86_64 (amd64)", value: "amd64" },
  { label: "Linux · ARM64 (aarch64)", value: "arm64" },
]

export const agentConfigFields = [
  { name: "server_url", type: "string", requirement: "required" },
  { name: "agent_token", type: "string", requirement: "required" },
  { name: "node_name", type: "string", requirement: "required" },
  { name: "advertised_host", type: "string", requirement: "required" },
  { name: "ssh_port", type: "integer", requirement: "ssh_default" },
  { name: "report_interval", type: "string", requirement: "required" },
] as const

export function normalizeServerURL(value: string): string | null {
  const input = value.trim()
  if (!/^https?:\/\//i.test(input) || /\s/.test(input)) return null
  try {
    const url = new URL(input)
    if (url.username || url.password || url.search || url.hash || url.pathname !== "/") return null
    return url.origin
  } catch (_) {
    void _
    return null
  }
}

export function buildAgentConfig(serverURL: string): string {
  return JSON.stringify({
    server_url: serverURL,
    agent_token: "REPLACE_WITH_NODE_AGENT_TOKEN",
    node_name: "node-one",
    advertised_host: "10.0.0.10",
    ssh_port: 22,
    report_interval: "5s",
  }, null, 2)
}

export function buildAgentCommand(architecture: AgentArchitecture): string {
  return [
    "umask 077",
    `CGO_ENABLED=0 GOOS=linux GOARCH=${architecture} go build -trimpath -o beat-agent ./cmd/agent`,
    "chmod 0700 ./beat-agent",
  ].join("\n")
}

export function buildHealthCommand(serverURL: string): string {
  const quotedURL = `'${`${serverURL}/readyz`.replaceAll("'", "'\\''")}'`
  return `curl --fail --show-error ${quotedURL}`
}

export const installAgentCommand = `sudo install -d -m 0700 /opt/beat-agent /etc/beat-agent
sudo install -m 0700 ./beat-agent /opt/beat-agent/beat-agent
sudo sh -c 'umask 077; touch /etc/beat-agent/agent.json'
sudo chmod 0600 /etc/beat-agent/agent.json`

export const startAgentCommand = `sudoedit /etc/beat-agent/agent.json
sudo /opt/beat-agent/beat-agent --config /etc/beat-agent/agent.json`

export const agentServiceUnit = `[Unit]
Description=Beat monitoring agent
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=root
UMask=0077
ExecStart=/opt/beat-agent/beat-agent --config /etc/beat-agent/agent.json
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target`

export const prepareAgentServiceCommand = `sudo sh -c 'umask 077; touch /etc/systemd/system/beat-agent.service'
sudo chmod 0600 /etc/systemd/system/beat-agent.service
sudoedit /etc/systemd/system/beat-agent.service`

export const enableAgentServiceCommand = `sudo systemctl daemon-reload
sudo systemctl enable --now beat-agent
sudo systemctl status beat-agent --no-pager
sudo journalctl -u beat-agent -n 50 --no-pager`

export const restartAgentServiceCommand = "sudo systemctl restart beat-agent"
