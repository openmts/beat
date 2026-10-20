// Package diagnosticprotocol 是服务端与 Agent 共用的诊断线路契约。
//
// 两侧必须对同一份能力文档产生完全相同的字节，才能让摘要比对成立；因此这里
// 保存类型定义、规范序列化与校验，而不是让两端各自复制一份结构体。
package diagnosticprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	// Version 是能力文档版本；不匹配时整份文档无效。
	Version = 1
	// LeaseDuration 与心跳宽限与远端运维租约语义保持一致。
	LeaseDuration = 30 * time.Second
	// MaximumDocumentBytes 限制能力文档大小，防止异常上报撑爆 SQLite。
	MaximumDocumentBytes = 16384
	maximumBinaryLength  = 260
)

var ErrInvalid = errors.New("invalid diagnostic protocol message")

// MTRCapability 与 Iperf3Capability 只描述可验证的本地能力。
type MTRCapability struct {
	Binary     string   `json:"binary"`
	Version    string   `json:"version"`
	IPFamilies []string `json:"ip_families"`
	Protocols  []string `json:"protocols"`
}

type Iperf3Capability struct {
	Binary     string   `json:"binary"`
	Version    string   `json:"version"`
	Protocols  []string `json:"protocols"`
	Directions []string `json:"directions"`
}

// CapabilityMaxima 是 Agent 本地上限；服务端只能收紧，不能扩大。
type CapabilityMaxima struct {
	Probes          int `json:"probes"`
	HopLimit        int `json:"hop_limit"`
	DurationSeconds int `json:"duration_seconds"`
	ParallelStreams int `json:"parallel_streams"`
	BitrateMbps     int `json:"bitrate_mbps"`
}

// CapabilityDocument 是 Agent 上报的有界能力文档。
type CapabilityDocument struct {
	MTR     *MTRCapability    `json:"mtr,omitempty"`
	Iperf3  *Iperf3Capability `json:"iperf3,omitempty"`
	Kinds   []string          `json:"kinds"`
	State   string            `json:"state"`
	Version int               `json:"version"`
	Maxima  CapabilityMaxima  `json:"maxima"`
}

// MarshalCanonical 是唯一的序列化入口；两端都必须使用它计算摘要。
func (document CapabilityDocument) MarshalCanonical() ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(document)
	if err != nil || len(encoded) > MaximumDocumentBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

// Digest 返回能力文档摘要；服务端把它与租约请求中的摘要逐字比较。
func (document CapabilityDocument) Digest() (string, error) {
	encoded, err := document.MarshalCanonical()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

// Validate 校验能力文档的边界；任何越界都使整份文档无效。
func (document CapabilityDocument) Validate() error {
	if document.Version != Version || len(document.Kinds) == 0 || len(document.Kinds) > 2 {
		return ErrInvalid
	}
	for _, kind := range document.Kinds {
		if kind != model.DiagnosticKindMTR && kind != model.DiagnosticKindIperf3 {
			return ErrInvalid
		}
	}
	if err := document.Maxima.Validate(); err != nil {
		return err
	}
	if slices.Contains(document.Kinds, model.DiagnosticKindMTR) {
		if document.MTR == nil || !validBinary(document.MTR.Binary) || !validIPFamilies(document.MTR.IPFamilies) ||
			!validProtocols(document.MTR.Protocols, false) {
			return ErrInvalid
		}
	}
	if slices.Contains(document.Kinds, model.DiagnosticKindIperf3) {
		if document.Iperf3 == nil || !validBinary(document.Iperf3.Binary) || !validProtocols(document.Iperf3.Protocols, true) ||
			!validDirections(document.Iperf3.Directions) {
			return ErrInvalid
		}
	}
	return nil
}

// Supports 报告能力是否覆盖请求的种类、协议、方向与参数上界。
func (document CapabilityDocument) Supports(kind, protocol, direction string, probes, hopLimit, duration, streams, bitrate int) bool {
	if !slices.Contains(document.Kinds, kind) {
		return false
	}
	maxima := document.Maxima
	if kind == model.DiagnosticKindMTR {
		return document.MTR != nil && slices.Contains(document.MTR.Protocols, protocol) &&
			probes <= maxima.Probes && hopLimit <= maxima.HopLimit
	}
	return document.Iperf3 != nil && slices.Contains(document.Iperf3.Protocols, protocol) &&
		slices.Contains(document.Iperf3.Directions, direction) && duration <= maxima.DurationSeconds &&
		streams <= maxima.ParallelStreams && bitrate <= maxima.BitrateMbps
}

func (maxima CapabilityMaxima) Validate() error {
	if maxima.Probes < model.MinimumDiagnosticProbes || maxima.Probes > model.MaximumDiagnosticProbes {
		return ErrInvalid
	}
	if maxima.HopLimit < 1 || maxima.HopLimit > model.MaximumDiagnosticHops {
		return ErrInvalid
	}
	if maxima.DurationSeconds < model.MinimumDiagnosticDurationSeconds ||
		maxima.DurationSeconds > model.HardMaximumDiagnosticDurationSeconds {
		return ErrInvalid
	}
	if maxima.ParallelStreams < model.MinimumDiagnosticStreams || maxima.ParallelStreams > model.MaximumDiagnosticStreams {
		return ErrInvalid
	}
	if maxima.BitrateMbps < 1 || maxima.BitrateMbps > model.MaximumDiagnosticBitrateMbps {
		return ErrInvalid
	}
	return nil
}

func validBinary(path string) bool {
	if path == "" || len(path) > maximumBinaryLength || !filepath.IsAbs(path) {
		return false
	}
	return !strings.ContainsAny(path, "\x00\n\r") && !strings.Contains(path, "..")
}

func validIPFamilies(families []string) bool {
	if len(families) == 0 || len(families) > 2 {
		return false
	}
	for _, family := range families {
		if family != model.IPFamilyIPv4 && family != model.IPFamilyIPv6 {
			return false
		}
	}
	return true
}

func validProtocols(protocols []string, streamsOnly bool) bool {
	if len(protocols) == 0 {
		return false
	}
	for _, protocol := range protocols {
		switch protocol {
		case model.DiagnosticProtocolICMP:
			if streamsOnly {
				return false
			}
		case model.DiagnosticProtocolTCP, model.DiagnosticProtocolUDP:
		default:
			return false
		}
	}
	return true
}

func validDirections(directions []string) bool {
	if len(directions) == 0 {
		return false
	}
	for _, direction := range directions {
		if direction != model.DiagnosticDirectionForward && direction != model.DiagnosticDirectionReverse {
			return false
		}
	}
	return true
}
