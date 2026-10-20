package agentrollout

import "time"

// artifactFileSuffix 是内容寻址的产物文件名后缀，按 SHA-256 摘要去重共享。
const artifactFileSuffix = ".artifact"

const (
	maximumTrustKeys              = 32
	maximumIdentifierBytes        = 128
	maximumUserAgentBytes         = 512
	assignmentNonceBytes          = 32
	maximumObservationCounter     = 1_000_000
	requiredArtifactCopies        = 2
	defaultCanaryCount            = 1
	defaultWaveSize               = 10
	defaultConcurrentAssignments  = 1
	maximumConcurrentAssignments  = 100
	defaultObservationSeconds     = 300
	minimumObservationSeconds     = 30
	maximumObservationSeconds     = 3600
	defaultReportFreshnessSeconds = 60
	minimumReportFreshnessSeconds = 5
	maximumReportFreshnessSeconds = 3600
	defaultRolloutWindow          = 7 * 24 * time.Hour
	maximumRolloutWindow          = 30 * 24 * time.Hour
	recoveryBatchSize             = 1000
)
