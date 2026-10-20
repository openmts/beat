package model

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"unicode/utf8"
)

// Validate 校验服务端策略的有界取值。
func (policy DiagnosticPolicy) Validate() error {
	if len(policy.AllowedKinds) == 0 {
		return errors.New("diagnostic policy must allow at least one kind")
	}
	for _, kind := range policy.AllowedKinds {
		if !validDiagnosticKind(kind) {
			return fmt.Errorf("diagnostic policy kind %q is unsupported", kind)
		}
	}
	if policy.MaximumActiveJobs < 1 || policy.MaximumActiveJobs > HardMaximumActiveDiagnosticJobs {
		return fmt.Errorf("diagnostic policy maximum active jobs must be 1-%d", HardMaximumActiveDiagnosticJobs)
	}
	if policy.MaximumActiveJobsPerNode < 1 || policy.MaximumActiveJobsPerNode > policy.MaximumActiveJobs {
		return errors.New("diagnostic policy per-node limit is invalid")
	}
	if policy.DailyByteBudget < 0 {
		return errors.New("diagnostic policy daily byte budget is invalid")
	}
	if policy.MaximumIperfDuration < MinimumDiagnosticDurationSeconds ||
		policy.MaximumIperfDuration > HardMaximumDiagnosticDurationSeconds {
		return errors.New("diagnostic policy iperf3 duration is invalid")
	}
	if policy.MaximumIperfBitrate < 1 || policy.MaximumIperfBitrate > MaximumDiagnosticBitrateMbps {
		return errors.New("diagnostic policy iperf3 bitrate is invalid")
	}
	return nil
}

// Validate 校验目标记录的名称、地址、协议与有界取值。
func (target DiagnosticTarget) Validate() error {
	if strings.TrimSpace(target.Name) == "" ||
		utf8.RuneCountInString(target.Name) > MaximumDiagnosticNameLength {
		return errors.New("diagnostic target name must contain 1 to 100 characters")
	}
	if utf8.RuneCountInString(target.Purpose) > MaximumDiagnosticPurposeLength {
		return errors.New("diagnostic target purpose is too long")
	}
	if !validDiagnosticKindList(target.Kinds) {
		return errors.New("diagnostic target kinds are invalid")
	}
	if err := validateDiagnosticAddress(target.Address); err != nil {
		return err
	}
	if target.Port < 0 || target.Port > 65535 {
		return errors.New("diagnostic target port is invalid")
	}
	if target.IPFamily != IPFamilyAuto && target.IPFamily != IPFamilyIPv4 && target.IPFamily != IPFamilyIPv6 {
		return errors.New("diagnostic target address family is invalid")
	}
	if !validDiagnosticProtocol(target.Protocol) {
		return errors.New("diagnostic target protocol is invalid")
	}
	if target.Direction != DiagnosticDirectionForward && target.Direction != DiagnosticDirectionReverse {
		return errors.New("diagnostic target direction is invalid")
	}
	if target.MaximumBitrateMbps < 1 || target.MaximumBitrateMbps > MaximumDiagnosticBitrateMbps {
		return errors.New("diagnostic target bitrate is invalid")
	}
	if target.MaximumDuration < MinimumDiagnosticDurationSeconds ||
		target.MaximumDuration > HardMaximumDiagnosticDurationSeconds {
		return errors.New("diagnostic target duration is invalid")
	}
	if target.MaximumParallelStream < MinimumDiagnosticStreams ||
		target.MaximumParallelStream > MaximumDiagnosticStreams {
		return errors.New("diagnostic target parallel streams are invalid")
	}
	if target.DailyByteBudget < 0 {
		return errors.New("diagnostic target daily byte budget is invalid")
	}
	return validateDiagnosticAllowlist(target.AllowedAddresses)
}

func validateDiagnosticAddress(address string) error {
	if address == "" || len(address) > MaximumDiagnosticAddressBytes {
		return errors.New("diagnostic target address is invalid")
	}
	if strings.ContainsAny(address, "/\\ \t\r\n\"'`") {
		return errors.New("diagnostic target address is invalid")
	}
	for _, character := range address {
		if character < 0x20 || character == 0x7f {
			return errors.New("diagnostic target address is invalid")
		}
	}
	return nil
}

func validateDiagnosticAllowlist(entries []string) error {
	if len(entries) > MaximumDiagnosticTargetAddresses {
		return errors.New("diagnostic target allowlist is too long")
	}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return fmt.Errorf("diagnostic target allowlist entry %q is invalid", entry)
		}
		if _, exists := seen[prefix.Masked().String()]; exists {
			return errors.New("diagnostic target allowlist contains duplicates")
		}
		seen[prefix.Masked().String()] = struct{}{}
	}
	return nil
}

func validDiagnosticKindList(kinds []string) bool {
	if len(kinds) == 0 || len(kinds) > 2 {
		return false
	}
	unique := slices.Clone(kinds)
	slices.Sort(unique)
	unique = slices.Compact(unique)
	if len(unique) != len(kinds) {
		return false
	}
	for _, kind := range kinds {
		if !validDiagnosticKind(kind) {
			return false
		}
	}
	return true
}

func validDiagnosticKind(kind string) bool {
	return kind == DiagnosticKindMTR || kind == DiagnosticKindIperf3
}

func validDiagnosticProtocol(protocol string) bool {
	switch protocol {
	case DiagnosticProtocolICMP, DiagnosticProtocolTCP, DiagnosticProtocolUDP:
		return true
	default:
		return false
	}
}
