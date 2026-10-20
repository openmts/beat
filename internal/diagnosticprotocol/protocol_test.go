package diagnosticprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func validCapabilityDocument() CapabilityDocument {
	return CapabilityDocument{
		MTR:     &MTRCapability{Binary: "/usr/bin/mtr", Version: "mtr 0.95", IPFamilies: []string{model.IPFamilyIPv4}, Protocols: []string{model.DiagnosticProtocolICMP}},
		Iperf3:  &Iperf3Capability{Binary: "/usr/bin/iperf3", Version: "iperf 3.17", Protocols: []string{model.DiagnosticProtocolTCP, model.DiagnosticProtocolUDP}, Directions: []string{model.DiagnosticDirectionForward}},
		Kinds:   []string{model.DiagnosticKindMTR, model.DiagnosticKindIperf3},
		State:   "ready",
		Version: Version,
		Maxima: CapabilityMaxima{Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops,
			DurationSeconds: model.DefaultIperfDurationSeconds, ParallelStreams: model.MinimumDiagnosticStreams,
			BitrateMbps: model.DefaultDiagnosticBitrateMbps},
	}
}

func TestCapabilityDocumentCanonicalEncodingAndDigest(t *testing.T) {
	document := validCapabilityDocument()
	encoded, err := document.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 || len(encoded) > MaximumDocumentBytes {
		t.Fatalf("encoded size = %d", len(encoded))
	}
	var decoded CapabilityDocument
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	// 规范序列化必须稳定：同一份文档两次摘要一致，不同文档摘要不同。
	digest, err := document.Digest()
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(encoded)
	if digest != hex.EncodeToString(expected[:]) {
		t.Fatalf("digest = %s", digest)
	}
	repeated, err := document.Digest()
	if err != nil || repeated != digest {
		t.Fatalf("digest not stable: %s / %v", repeated, err)
	}
	other := validCapabilityDocument()
	other.Maxima.Probes = model.MaximumDiagnosticProbes
	otherDigest, err := other.Digest()
	if err != nil || otherDigest == digest {
		t.Fatalf("digest collision: %s / %v", otherDigest, err)
	}
	// 超过文档上限时既不算摘要也不序列化。
	oversized := validCapabilityDocument()
	oversized.MTR.Protocols = make([]string, 0, 4096)
	for index := 0; index < 4096; index += 1 {
		oversized.MTR.Protocols = append(oversized.MTR.Protocols, model.DiagnosticProtocolTCP)
	}
	if _, err := oversized.MarshalCanonical(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized document = %v", err)
	}
	if _, err := oversized.Digest(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized digest = %v", err)
	}
}

func TestCapabilityDocumentValidation(t *testing.T) {
	if err := validCapabilityDocument().Validate(); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	mtrOnly := validCapabilityDocument()
	mtrOnly.Kinds, mtrOnly.Iperf3 = []string{model.DiagnosticKindMTR}, nil
	if err := mtrOnly.Validate(); err != nil {
		t.Fatalf("mtr-only document rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*CapabilityDocument)
	}{
		{"version mismatch", func(document *CapabilityDocument) { document.Version = Version + 1 }},
		{"no kinds", func(document *CapabilityDocument) { document.Kinds = nil }},
		{"too many kinds", func(document *CapabilityDocument) { document.Kinds = []string{"mtr", "iperf3", "mtr"} }},
		{"unknown kind", func(document *CapabilityDocument) { document.Kinds = []string{"traceroute"} }},
		{"mtr without block", func(document *CapabilityDocument) { document.MTR = nil }},
		{"mtr relative binary", func(document *CapabilityDocument) { document.MTR.Binary = "usr/bin/mtr" }},
		{"mtr binary with traversal", func(document *CapabilityDocument) { document.MTR.Binary = "/usr/../bin/mtr" }},
		{"mtr binary with newline", func(document *CapabilityDocument) { document.MTR.Binary = "/usr/bin/mtr\n" }},
		{"mtr binary too long", func(document *CapabilityDocument) {
			document.MTR.Binary = "/" + strings.Repeat("b", maximumBinaryLength)
		}},
		{"mtr without ip family", func(document *CapabilityDocument) { document.MTR.IPFamilies = nil }},
		{"mtr too many ip families", func(document *CapabilityDocument) {
			document.MTR.IPFamilies = []string{model.IPFamilyIPv4, model.IPFamilyIPv6, model.IPFamilyIPv4}
		}},
		{"mtr unknown ip family", func(document *CapabilityDocument) { document.MTR.IPFamilies = []string{"ipx"} }},
		{"mtr without protocol", func(document *CapabilityDocument) { document.MTR.Protocols = nil }},
		{"mtr unknown protocol", func(document *CapabilityDocument) { document.MTR.Protocols = []string{"sctp"} }},
		{"iperf without block", func(document *CapabilityDocument) { document.Iperf3 = nil }},
		{"iperf icmp", func(document *CapabilityDocument) { document.Iperf3.Protocols = []string{model.DiagnosticProtocolICMP} }},
		{"iperf without directions", func(document *CapabilityDocument) { document.Iperf3.Directions = nil }},
		{"iperf unknown direction", func(document *CapabilityDocument) { document.Iperf3.Directions = []string{"sideways"} }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			document := validCapabilityDocument()
			testCase.mutate(&document)
			if err := document.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// 只声明 mtr 时不需要 iperf3 块，反之亦然。
	iperfOnly := validCapabilityDocument()
	iperfOnly.Kinds, iperfOnly.MTR = []string{model.DiagnosticKindIperf3}, nil
	if err := iperfOnly.Validate(); err != nil {
		t.Fatalf("iperf-only document rejected: %v", err)
	}
}

func TestCapabilityMaximaValidation(t *testing.T) {
	valid := CapabilityMaxima{Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops,
		DurationSeconds: model.DefaultIperfDurationSeconds, ParallelStreams: model.MinimumDiagnosticStreams,
		BitrateMbps: model.DefaultDiagnosticBitrateMbps}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid maxima rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*CapabilityMaxima)
	}{
		{"probes below minimum", func(maxima *CapabilityMaxima) { maxima.Probes = model.MinimumDiagnosticProbes - 1 }},
		{"probes above maximum", func(maxima *CapabilityMaxima) { maxima.Probes = model.MaximumDiagnosticProbes + 1 }},
		{"hop limit zero", func(maxima *CapabilityMaxima) { maxima.HopLimit = 0 }},
		{"hop limit above maximum", func(maxima *CapabilityMaxima) { maxima.HopLimit = model.MaximumDiagnosticHops + 1 }},
		{"duration too short", func(maxima *CapabilityMaxima) { maxima.DurationSeconds = model.MinimumDiagnosticDurationSeconds - 1 }},
		{"duration too long", func(maxima *CapabilityMaxima) {
			maxima.DurationSeconds = model.HardMaximumDiagnosticDurationSeconds + 1
		}},
		{"streams zero", func(maxima *CapabilityMaxima) { maxima.ParallelStreams = 0 }},
		{"streams above maximum", func(maxima *CapabilityMaxima) { maxima.ParallelStreams = model.MaximumDiagnosticStreams + 1 }},
		{"bitrate zero", func(maxima *CapabilityMaxima) { maxima.BitrateMbps = 0 }},
		{"bitrate above maximum", func(maxima *CapabilityMaxima) { maxima.BitrateMbps = model.MaximumDiagnosticBitrateMbps + 1 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			maxima := valid
			testCase.mutate(&maxima)
			if err := maxima.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	bounds := CapabilityMaxima{Probes: model.MaximumDiagnosticProbes, HopLimit: model.MaximumDiagnosticHops,
		DurationSeconds: model.HardMaximumDiagnosticDurationSeconds, ParallelStreams: model.MaximumDiagnosticStreams,
		BitrateMbps: model.MaximumDiagnosticBitrateMbps}
	if err := bounds.Validate(); err != nil {
		t.Fatalf("boundary maxima rejected: %v", err)
	}
}

func TestCapabilityDocumentSupports(t *testing.T) {
	document := validCapabilityDocument()
	if !document.Supports(model.DiagnosticKindMTR, model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward,
		model.DefaultDiagnosticProbes, model.DefaultDiagnosticHops, 0, 0, 0) {
		t.Fatal("mtr request not supported")
	}
	if document.Supports(model.DiagnosticKindMTR, model.DiagnosticProtocolTCP, model.DiagnosticDirectionForward,
		model.DefaultDiagnosticProbes, model.DefaultDiagnosticHops, 0, 0, 0) {
		t.Fatal("unsupported mtr protocol accepted")
	}
	if document.Supports(model.DiagnosticKindMTR, model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward,
		model.MaximumDiagnosticProbes+1, model.DefaultDiagnosticHops, 0, 0, 0) {
		t.Fatal("probes above local maximum accepted")
	}
	if document.Supports(model.DiagnosticKindMTR, model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward,
		model.DefaultDiagnosticProbes, model.MaximumDiagnosticHops+1, 0, 0, 0) {
		t.Fatal("hop limit above local maximum accepted")
	}
	if !document.Supports(model.DiagnosticKindIperf3, model.DiagnosticProtocolTCP, model.DiagnosticDirectionForward,
		0, 0, model.DefaultIperfDurationSeconds, model.MinimumDiagnosticStreams, model.DefaultDiagnosticBitrateMbps) {
		t.Fatal("iperf request not supported")
	}
	for name, request := range map[string]struct {
		protocol, direction            string
		duration, streams, bitrateMbps int
	}{
		"unsupported protocol":  {model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward, 1, 1, 1},
		"unsupported direction": {model.DiagnosticProtocolTCP, model.DiagnosticDirectionReverse, 1, 1, 1},
		"duration too long":     {model.DiagnosticProtocolTCP, model.DiagnosticDirectionForward, model.DefaultIperfDurationSeconds + 1, 1, 1},
		"streams too high":      {model.DiagnosticProtocolTCP, model.DiagnosticDirectionForward, 1, model.MinimumDiagnosticStreams + 1, 1},
		"bitrate too high":      {model.DiagnosticProtocolTCP, model.DiagnosticDirectionForward, 1, 1, model.DefaultDiagnosticBitrateMbps + 1},
	} {
		if document.Supports(model.DiagnosticKindIperf3, request.protocol, request.direction,
			0, 0, request.duration, request.streams, request.bitrateMbps) {
			t.Fatalf("%s accepted", name)
		}
	}
	if document.Supports("traceroute", model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward, 1, 1, 0, 0, 0) {
		t.Fatal("unknown kind accepted")
	}
	// 声明了种类但缺少对应能力块时必须判为不支持。
	broken := validCapabilityDocument()
	broken.MTR = nil
	if broken.Supports(model.DiagnosticKindMTR, model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward, 1, 1, 0, 0, 0) {
		t.Fatal("missing mtr block ignored")
	}
	broken = validCapabilityDocument()
	broken.Iperf3 = nil
	if broken.Supports(model.DiagnosticKindIperf3, model.DiagnosticProtocolTCP, model.DiagnosticDirectionForward, 0, 0, 1, 1, 1) {
		t.Fatal("missing iperf block ignored")
	}
}
