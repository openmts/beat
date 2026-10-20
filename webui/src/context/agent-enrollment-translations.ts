import type { Catalog } from "@/context/locales"

export const agentEnrollmentTranslations: Catalog = {
  en: {
    "enrollment.tab": "One-command setup",
    "enrollment.title": "Generate a join command",
    "enrollment.description": "Copy one command to the monitored machine. The agent registers the node, writes its configuration and starts a systemd service.",
    "enrollment.prerequisite": "The target must be a Linux machine with systemd that already has the beat-agent executable matching this server. Run the command with sudo from the directory containing the executable; node creation and configuration are automatic.",
    "enrollment.allow_http": "Allow HTTP on this trusted network. Enrollment credentials will be sent without transport encryption; use HTTPS across untrusted networks.",
    "enrollment.generate": "Generate join command",
    "enrollment.regenerate": "Generate another command",
    "enrollment.generating": "Generating…",
    "enrollment.generate_error": "Could not generate a join command. Try again.",
    "enrollment.command": "Join command",
    "enrollment.lifetime": "Every generation creates a different token, valid for 30 minutes and one machine. Save the command before leaving this page. Generating another command does not cancel a previously issued one.",
    "enrollment.expires": "Expires at",
    "enrollment.expired": "This command has expired. Generate a new one.",
    "enrollment.run_hint": "Run this on the target machine. A successful result confirms that the node is registered and beat-agent.service is active; then check the node's heartbeat and metrics in Manage nodes.",
    "enrollment.automatic_title": "What the command sets up",
    "enrollment.automatic_identify": "Uses the hostname as the node name, detects the SSH port, and lets the server determine the advertised address from the connection. A name conflict never replaces an existing node.",
    "enrollment.automatic_config": "Registers a dedicated reporting credential and saves /etc/beat-agent/agent.json with mode 0600. Reporting defaults to every 5 seconds.",
    "enrollment.automatic_systemd": "Installs the executable at /opt/beat-agent/beat-agent and writes beat-agent.service. The root service starts now and at boot, and restarts after failures.",
    "enrollment.verify": "The join token only authorizes enrollment. Once connected, the service uses its own node credential for ongoing reports; only one agent should run for each node.",
    "enrollment.recovery_title": "Customize or resume an installation",
    "enrollment.recovery": "After an interruption, rerun the same command on the same machine. If it expired before setup finished, generate a new command; saved identity prevents duplicate nodes. With a valid agent.json or a complete local enrollment receipt, recover the local service with:",
    "enrollment.service_command": "Install the local service",
    "enrollment.customize": "Append --name for a different node name, --host for a server-reachable SSH address, or --ssh-port for a forwarded port. Join refuses to overwrite an unrelated configuration or systemd unit. See Configuration fields and Run & troubleshoot for maintenance.",
  },
}
