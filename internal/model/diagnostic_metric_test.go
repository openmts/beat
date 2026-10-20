package model

import (
	"errors"
	"strings"
	"testing"
)

func validMTRSample() DiagnosticSample {
	return DiagnosticSample{JobID: "job-1", NodeID: "node-1", Metric: DiagnosticMetricMTRLatencyMS,
		Hop: 3, Stat: DiagnosticStatAvg, Value: 12.5}
}

func validIperfSample() DiagnosticSample {
	return DiagnosticSample{JobID: "job-1", NodeID: "node-1", Metric: DiagnosticMetricIperfBytesPerSecond,
		Direction: DiagnosticDirectionForward, Protocol: DiagnosticProtocolTCP, Role: DiagnosticRoleSender, Value: 1000}
}

func TestDiagnosticMetricRegistry(t *testing.T) {
	metrics := DiagnosticMetrics()
	if len(metrics) != 9 {
		t.Fatalf("registry = %v", metrics)
	}
	for _, metric := range metrics {
		if !ValidDiagnosticMetric(metric) {
			t.Fatalf("%s not registered", metric)
		}
	}
	if IsMTRDiagnosticMetric(DiagnosticMetricIperfBytes) || IsIperfDiagnosticMetric(DiagnosticMetricMTRLossPercent) {
		t.Fatal("registry families overlap")
	}
	if ValidDiagnosticMetric("cpu") || ValidDiagnosticMetric("") {
		t.Fatal("unknown metric accepted")
	}
	// 返回副本：调用方改动不会污染内部登记表。
	metrics[0] = "mutated"
	if DiagnosticMetrics()[0] == "mutated" {
		t.Fatal("registry leaked its backing slice")
	}
}

func TestDiagnosticSampleTags(t *testing.T) {
	mtrTags, err := validMTRSample().Tags()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"node": "node-1", "job": "job-1", "hop": "3", "stat": DiagnosticStatAvg}
	if len(mtrTags) != len(expected) {
		t.Fatalf("mtr tags = %v", mtrTags)
	}
	for key, value := range expected {
		if mtrTags[key] != value {
			t.Fatalf("mtr tags = %v", mtrTags)
		}
	}
	iperfTags, err := validIperfSample().Tags()
	if err != nil {
		t.Fatal(err)
	}
	if iperfTags["direction"] != DiagnosticDirectionForward || iperfTags["protocol"] != DiagnosticProtocolTCP ||
		iperfTags["role"] != DiagnosticRoleSender || len(iperfTags) != 5 {
		t.Fatalf("iperf tags = %v", iperfTags)
	}
}

func TestDiagnosticSampleRejectsInvalidRecords(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DiagnosticSample)
	}{
		{"missing job", func(sample *DiagnosticSample) { sample.JobID = "" }},
		{"job too long", func(sample *DiagnosticSample) { sample.JobID = strings.Repeat("j", MaximumDiagnosticReferenceBytes+1) }},
		{"missing node", func(sample *DiagnosticSample) { sample.NodeID = "" }},
		{"node too long", func(sample *DiagnosticSample) { sample.NodeID = strings.Repeat("n", MaximumDiagnosticReferenceBytes+1) }},
		{"unknown metric", func(sample *DiagnosticSample) { sample.Metric = "cpu" }},
		{"mtr without hop", func(sample *DiagnosticSample) { sample.Hop = 0 }},
		{"mtr hop above maximum", func(sample *DiagnosticSample) { sample.Hop = MaximumDiagnosticHops + 1 }},
		{"mtr unknown stat", func(sample *DiagnosticSample) { sample.Stat = "median" }},
		{"iperf unknown direction", func(sample *DiagnosticSample) {
			iperf := validIperfSample()
			iperf.Direction = "sideways"
			*sample = iperf
		}},
		{"iperf without direction", func(sample *DiagnosticSample) { *sample = validIperfSample(); sample.Direction = "" }},
		{"iperf with icmp", func(sample *DiagnosticSample) { *sample = validIperfSample(); sample.Protocol = DiagnosticProtocolICMP }},
		{"iperf with unknown protocol", func(sample *DiagnosticSample) { *sample = validIperfSample(); sample.Protocol = "sctp" }},
		{"iperf unknown role", func(sample *DiagnosticSample) { *sample = validIperfSample(); sample.Role = "observer" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sample := validMTRSample()
			testCase.mutate(&sample)
			if _, err := sample.Tags(); !errors.Is(err, ErrDiagnosticSampleInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// 反向 iperf3 与 UDP 是合法组合，边界 hop 也合法。
	sample := validIperfSample()
	sample.Direction, sample.Protocol, sample.Role = DiagnosticDirectionReverse, DiagnosticProtocolUDP, DiagnosticRoleReceiver
	if _, err := sample.Tags(); err != nil {
		t.Fatalf("reverse udp sample rejected: %v", err)
	}
	mtr := validMTRSample()
	mtr.Hop, mtr.Stat = MaximumDiagnosticHops, DiagnosticStatWorst
	if _, err := mtr.Tags(); err != nil {
		t.Fatalf("boundary mtr sample rejected: %v", err)
	}
}

func TestValidateDiagnosticSamplesBoundsBatch(t *testing.T) {
	if err := ValidateDiagnosticSamples([]DiagnosticSample{validMTRSample()}); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	if err := ValidateDiagnosticSamples(nil); !errors.Is(err, ErrDiagnosticSampleLimit) {
		t.Fatalf("empty batch = %v", err)
	}
	oversized := make([]DiagnosticSample, MaximumDiagnosticSampleBatch+1)
	for index := range oversized {
		oversized[index] = validIperfSample()
	}
	if err := ValidateDiagnosticSamples(oversized); !errors.Is(err, ErrDiagnosticSampleLimit) {
		t.Fatalf("oversized batch = %v", err)
	}
	// 批量边界（512 条）本身合法，批内非法样本按样本错误上报。
	if err := ValidateDiagnosticSamples(oversized[:MaximumDiagnosticSampleBatch]); err != nil {
		t.Fatalf("boundary batch rejected: %v", err)
	}
	broken := []DiagnosticSample{validMTRSample(), {}}
	if err := ValidateDiagnosticSamples(broken); !errors.Is(err, ErrDiagnosticSampleInvalid) {
		t.Fatalf("broken batch = %v", err)
	}
}
