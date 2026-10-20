package handler

import (
	"github.com/beat/backend/internal/model"
)

const (
	securityDeliveryPending   = "pending"
	securityDeliveryDelivered = "delivered"
	securityDeliveryFailed    = "failed"
	securityDeliveryNone      = "none"
)

type securityOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type securityOptionsResponse struct {
	Kinds         []securityOption `json:"kinds"`
	Severities    []securityOption `json:"severities"`
	Reasons       []securityOption `json:"reasons"`
	Sources       []securityOption `json:"sources"`
	Deliveries    []securityOption `json:"deliveries"`
	RoutingStates []securityOption `json:"routing_states"`
	PrivacyLevels []securityOption `json:"privacy_levels"`
}

// securityOptions 返回固定选项词表；标签为稳定英文，界面按语言本地化。
func securityOptions() securityOptionsResponse {
	return securityOptionsResponse{
		Kinds: []securityOption{
			{model.SecurityEventLoginSucceeded, "Sign-in succeeded"},
			{model.SecurityEventLoginSuspicious, "Suspicious sign-in"},
		},
		Severities: []securityOption{
			{model.SecuritySeverityInfo, "Info"},
			{model.SecuritySeverityMedium, "Medium"},
			{model.SecuritySeverityHigh, "High"},
		},
		Reasons: []securityOption{
			{model.SecurityReasonRateLimitLockout, "Lockout threshold crossed"},
			{model.SecurityReasonInvalidTOTP, "Repeated invalid authenticator codes"},
			{model.SecurityReasonNewSourcePrefix, "New source network"},
			{model.SecurityReasonNewDeviceFamily, "New device family"},
			{model.SecurityReasonDisabledIdentity, "Disabled or revoked identity"},
			{model.SecurityReasonDistantSource, "Concurrent distant source"},
		},
		Sources: []securityOption{
			{model.SecuritySourceClassPublic, "Public network"},
			{model.SecuritySourceClassPrivate, "Private network"},
			{model.SecuritySourceClassLoopback, "Loopback"},
			{model.SecuritySourceClassLinkLocal, "Link-local"},
			{model.SecuritySourceClassUnknown, "Unknown"},
		},
		Deliveries: []securityOption{
			{securityDeliveryPending, "Pending"},
			{securityDeliveryDelivered, "Delivered"},
			{securityDeliveryFailed, "Failed"},
			{securityDeliveryNone, "Not routed"},
		},
		RoutingStates: []securityOption{
			{model.SecurityRoutingRouted, "Routed"},
			{model.SecurityRoutingNotRouted, "Not routed"},
			{model.SecurityRoutingSkipped, "Skipped by policy"},
			{model.SecurityRoutingUnavailable, "Queue unavailable"},
		},
		PrivacyLevels: []securityOption{
			{"coarse", "Coarse source (recommended)"},
			{"full_source", "Full source address"},
		},
	}
}
