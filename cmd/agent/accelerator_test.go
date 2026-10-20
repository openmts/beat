package main

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/acceleratorcollect"
	"github.com/beat/backend/internal/agent"
)

func TestAcceleratorComponentCancellationIsNormalShutdown(t *testing.T) {
	collector, err := acceleratorcollect.New(acceleratorcollect.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = acceleratorComponent(collector)(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMonitoringBuildsGPUComponentAndRejectsInvalidLocalPolicy(t *testing.T) {
	config := agent.Config{ServerURL: "https://fixture.invalid", AgentToken: "fixture", NodeName: "fixture", AdvertisedHost: "host", SSHPort: 22, ReportInterval: time.Second, Accelerators: &acceleratorcollect.Config{Enabled: true}}
	components, err := monitoringComponents(config)
	if err != nil || len(components) != 4 {
		t.Fatal(len(components), err)
	}
	config.Accelerators.SampleIntervalSeconds = 1
	if _, err = monitoringComponents(config); err == nil {
		t.Fatal("invalid GPU policy admitted")
	}
}
