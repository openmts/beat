package main

import (
	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrolloutremote"
)

const (
	agentReleaseRepositoryEnv = "BEAT_AGENT_RELEASE_REPOSITORY"
	agentReleaseAPIURLEnv     = "BEAT_AGENT_RELEASE_API_URL"
	agentReleaseTokenEnv      = "BEAT_AGENT_RELEASE_TOKEN"
)

func newAgentReleaseRemote(service *agentrollout.Service, environment func(string) string) (*agentrolloutremote.Service, error) {
	repository := environment(agentReleaseRepositoryEnv)
	if repository == "" {
		repository = agentrolloutremote.DefaultRepository
	}
	apiURL := environment(agentReleaseAPIURLEnv)
	if apiURL == "" {
		apiURL = agentrolloutremote.DefaultAPIURL
	}
	remote, err := agentrolloutremote.New(agentrolloutremote.Config{
		Rollout: service, Repository: repository, APIURL: apiURL, Token: environment(agentReleaseTokenEnv),
	})
	if err != nil {
		return nil, errRolloutConfiguration
	}
	return remote, nil
}
