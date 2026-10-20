package model

import (
	"net"
	"strings"
	"testing"
)

func validDiagnosticTarget() DiagnosticTarget {
	return DiagnosticTarget{Name: "edge router", Purpose: "path quality", Kinds: []string{DiagnosticKindMTR},
		Address: "192.0.2.10", Port: 0, IPFamily: IPFamilyAuto, Protocol: DiagnosticProtocolICMP,
		Direction: DiagnosticDirectionForward, AllowedAddresses: []string{"192.0.2.0/24"},
		MaximumBitrateMbps: DefaultDiagnosticBitrateMbps, MaximumDuration: DefaultIperfDurationSeconds,
		MaximumParallelStream: MinimumDiagnosticStreams}
}

func TestDiagnosticPolicyValidation(t *testing.T) {
	if err := DefaultDiagnosticPolicy().Validate(); err != nil {
		t.Fatalf("default policy rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*DiagnosticPolicy)
	}{
		{"no kinds", func(policy *DiagnosticPolicy) { policy.AllowedKinds = nil }},
		{"unsupported kind", func(policy *DiagnosticPolicy) { policy.AllowedKinds = []string{"traceroute"} }},
		{"zero active jobs", func(policy *DiagnosticPolicy) { policy.MaximumActiveJobs = 0 }},
		{"active jobs above hard limit", func(policy *DiagnosticPolicy) { policy.MaximumActiveJobs = HardMaximumActiveDiagnosticJobs + 1 }},
		{"per node limit zero", func(policy *DiagnosticPolicy) { policy.MaximumActiveJobsPerNode = 0 }},
		{"per node above global", func(policy *DiagnosticPolicy) { policy.MaximumActiveJobsPerNode = policy.MaximumActiveJobs + 1 }},
		{"negative daily budget", func(policy *DiagnosticPolicy) { policy.DailyByteBudget = -1 }},
		{"iperf duration too short", func(policy *DiagnosticPolicy) { policy.MaximumIperfDuration = MinimumDiagnosticDurationSeconds - 1 }},
		{"iperf duration too long", func(policy *DiagnosticPolicy) { policy.MaximumIperfDuration = HardMaximumDiagnosticDurationSeconds + 1 }},
		{"iperf bitrate zero", func(policy *DiagnosticPolicy) { policy.MaximumIperfBitrate = 0 }},
		{"iperf bitrate above maximum", func(policy *DiagnosticPolicy) { policy.MaximumIperfBitrate = MaximumDiagnosticBitrateMbps + 1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			policy := DefaultDiagnosticPolicy()
			testCase.mutate(&policy)
			if err := policy.Validate(); err == nil {
				t.Fatalf("policy accepted: %+v", policy)
			}
		})
	}
	// 允许 MTR + iperf3 两种受控种类，且 iperf3 上限可以顶到硬边界。
	policy := DefaultDiagnosticPolicy()
	policy.AllowedKinds = []string{DiagnosticKindMTR, DiagnosticKindIperf3}
	policy.MaximumActiveJobs, policy.MaximumActiveJobsPerNode = HardMaximumActiveDiagnosticJobs, 2
	policy.MaximumIperfDuration, policy.MaximumIperfBitrate = HardMaximumDiagnosticDurationSeconds, MaximumDiagnosticBitrateMbps
	policy.DailyByteBudget = 1 << 30
	if err := policy.Validate(); err != nil {
		t.Fatalf("bounded policy rejected: %v", err)
	}
}

func TestDiagnosticTargetValidation(t *testing.T) {
	if err := validDiagnosticTarget().Validate(); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*DiagnosticTarget)
	}{
		{"blank name", func(target *DiagnosticTarget) { target.Name = "   " }},
		{"name too long", func(target *DiagnosticTarget) { target.Name = strings.Repeat("测", MaximumDiagnosticNameLength+1) }},
		{"purpose too long", func(target *DiagnosticTarget) { target.Purpose = strings.Repeat("p", MaximumDiagnosticPurposeLength+1) }},
		{"no kinds", func(target *DiagnosticTarget) { target.Kinds = nil }},
		{"too many kinds", func(target *DiagnosticTarget) {
			target.Kinds = []string{DiagnosticKindMTR, DiagnosticKindIperf3, DiagnosticKindMTR}
		}},
		{"duplicate kinds", func(target *DiagnosticTarget) {
			target.Kinds = []string{DiagnosticKindMTR, DiagnosticKindMTR}
		}},
		{"unsupported kind", func(target *DiagnosticTarget) { target.Kinds = []string{"dig"} }},
		{"empty address", func(target *DiagnosticTarget) { target.Address = "" }},
		{"address too long", func(target *DiagnosticTarget) { target.Address = strings.Repeat("a", MaximumDiagnosticAddressBytes+1) }},
		{"address with separator", func(target *DiagnosticTarget) { target.Address = "192.0.2.10/24" }},
		{"address with quote", func(target *DiagnosticTarget) { target.Address = `"192.0.2.10"` }},
		{"address with control character", func(target *DiagnosticTarget) { target.Address = "192.0.2.10\x7f" }},
		{"negative port", func(target *DiagnosticTarget) { target.Port = -1 }},
		{"port above range", func(target *DiagnosticTarget) { target.Port = 65536 }},
		{"unknown ip family", func(target *DiagnosticTarget) { target.IPFamily = "ipx" }},
		{"unknown protocol", func(target *DiagnosticTarget) { target.Protocol = "sctp" }},
		{"unknown direction", func(target *DiagnosticTarget) { target.Direction = "sideways" }},
		{"bitrate zero", func(target *DiagnosticTarget) { target.MaximumBitrateMbps = 0 }},
		{"bitrate above maximum", func(target *DiagnosticTarget) { target.MaximumBitrateMbps = MaximumDiagnosticBitrateMbps + 1 }},
		{"duration too short", func(target *DiagnosticTarget) { target.MaximumDuration = MinimumDiagnosticDurationSeconds - 1 }},
		{"duration too long", func(target *DiagnosticTarget) { target.MaximumDuration = HardMaximumDiagnosticDurationSeconds + 1 }},
		{"streams zero", func(target *DiagnosticTarget) { target.MaximumParallelStream = 0 }},
		{"streams above maximum", func(target *DiagnosticTarget) { target.MaximumParallelStream = MaximumDiagnosticStreams + 1 }},
		{"negative daily budget", func(target *DiagnosticTarget) { target.DailyByteBudget = -1 }},
		{"allowlist entry invalid", func(target *DiagnosticTarget) { target.AllowedAddresses = []string{"not-a-prefix"} }},
		{"allowlist duplicates", func(target *DiagnosticTarget) { target.AllowedAddresses = []string{"192.0.2.0/24", "192.0.2.7/24"} }},
		{"allowlist too long", func(target *DiagnosticTarget) {
			entries := make([]string, 0, MaximumDiagnosticTargetAddresses+1)
			for index := 0; index <= MaximumDiagnosticTargetAddresses; index += 1 {
				entries = append(entries, net.IPv4(198, 51, 100, byte(index)).String()+"/32")
			}
			target.AllowedAddresses = entries
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			target := validDiagnosticTarget()
			testCase.mutate(&target)
			if err := target.Validate(); err == nil {
				t.Fatalf("target accepted: %+v", target)
			}
		})
	}
	// 边界值本身合法：最长名称、最大端口、双协议栈与靠上限的 iperf3 参数。
	target := validDiagnosticTarget()
	target.Name = strings.Repeat("n", MaximumDiagnosticNameLength)
	target.Kinds = []string{DiagnosticKindMTR, DiagnosticKindIperf3}
	target.Port, target.IPFamily = 65535, IPFamilyIPv6
	target.Protocol, target.Direction = DiagnosticProtocolUDP, DiagnosticDirectionReverse
	target.MaximumBitrateMbps, target.MaximumDuration = MaximumDiagnosticBitrateMbps, HardMaximumDiagnosticDurationSeconds
	target.MaximumParallelStream, target.DailyByteBudget = MaximumDiagnosticStreams, 1<<40
	target.AllowedAddresses = []string{"2001:db8::/32"}
	if err := target.Validate(); err != nil {
		t.Fatalf("boundary target rejected: %v", err)
	}
}
