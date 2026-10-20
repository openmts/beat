package main

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
)

func TestAgentReleaseRemoteEnvironmentDefaultsAndOverrides(t *testing.T) {
	service := &agentrollout.Service{}
	values := map[string]string{
		agentReleaseRepositoryEnv: "example/mirror",
		agentReleaseAPIURLEnv:     "https://github.example",
		agentReleaseTokenEnv:      "fixture-token",
	}
	remote, err := newAgentReleaseRemote(service, func(name string) string { return values[name] })
	if err != nil || remote == nil {
		t.Fatalf("remote = %v, %v", remote, err)
	}
	values = map[string]string{agentReleaseRepositoryEnv: "../unsafe"}
	if _, err := newAgentReleaseRemote(service, func(name string) string { return values[name] }); !errors.Is(err, errRolloutConfiguration) {
		t.Fatalf("unsafe repository = %v", err)
	}
}
