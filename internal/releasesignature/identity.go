package releasesignature

import (
	"regexp"
	"strings"
)

var releaseTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

var workflowPattern = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*/\.github/workflows/[A-Za-z0-9][A-Za-z0-9_.-]*\.ya?ml$`)

// ValidReleaseTag 将已验证manifest版本映射到唯一的CI tag，不接受路径或身份表达式。
func ValidReleaseTag(tag string) bool {
	if len(tag) > 128 || !releaseTagPattern.MatchString(tag) {
		return false
	}
	version := strings.SplitN(tag, "+", 2)[0]
	parts := strings.SplitN(version, "-", 2)
	if len(parts) == 1 {
		return true
	}
	for _, identifier := range strings.Split(parts[1], ".") {
		if len(identifier) > 1 && identifier[0] == '0' && strings.Trim(identifier, "0123456789") == "" {
			return false
		}
	}
	return true
}

func validIdentityPolicy(config Config) bool {
	if config.ReleaseWorkflow == "" {
		return validIdentity(config.CertificateIdentity)
	}
	return config.CertificateIdentity == "" && validIdentity(config.ReleaseWorkflow) && workflowPattern.MatchString(config.ReleaseWorkflow)
}

func (v *Verifier) identity(evidence Evidence) (string, error) {
	if v.config.ReleaseWorkflow == "" {
		return v.config.CertificateIdentity, nil
	}
	if !ValidReleaseTag(evidence.ReleaseTag) {
		return "", ErrRejected
	}
	return v.config.ReleaseWorkflow + "@refs/tags/" + evidence.ReleaseTag, nil
}
