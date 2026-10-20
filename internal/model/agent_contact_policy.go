package model

import "time"

const MinimumAgentOfflineAge = 90 * time.Second

const agentOfflinePeriods = 3

func AgentOfflineAfter(interval time.Duration) time.Duration {
	interval = min(max(interval, MinimumAgentReportInterval), MaximumAgentReportInterval)
	return max(MinimumAgentOfflineAge, interval*agentOfflinePeriods)
}
