package agentrollout

import "github.com/beat/backend/internal/agentrollout/protocol"

func compareVersion(target, current string) (int, bool) {
	return protocol.CompareVersions(target, current)
}

func ReleaseTag(version string) (string, error) { return protocol.ReleaseTag(version) }
