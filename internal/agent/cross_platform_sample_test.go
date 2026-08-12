package agent

import (
	"context"
	"testing"
)

func TestSystemSamplerCrossPlatform(t *testing.T) {
	sample, err := (SystemSampler{}).Sample(context.Background())
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if sample.CPU < 0 || sample.Memory <= 0 || sample.MemoryTotal <= 0 {
		t.Fatalf("invalid sample: %+v", sample)
	}
	if sample.CPUTotal <= 0 || sample.DiskTotal <= 0 {
		t.Fatalf("missing capacity: %+v", sample)
	}
	if sample.TCPConnections < 0 || sample.UDPConnections < 0 {
		t.Fatalf("negative connection counts: %+v", sample)
	}
	if sample.SystemInfo.OS == "" || sample.SystemInfo.Platform == "" {
		t.Fatalf("missing system info: %+v", sample.SystemInfo)
	}
}
