package store

import (
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

const maximumPreparedAgentReports = 128

type preparedAgentReport struct {
	receipt          model.AgentReceipt
	points           []mts.Point
	admittedAt       time.Time
	inUse, uncertain bool
}

func (s *MTSStore) reserveAgentPlan(attempt *AgentIngestAttempt, now time.Time) (*preparedAgentReport, error) {
	s.agentPreparedMu.Lock()
	defer s.agentPreparedMu.Unlock()
	if s.agentPrepared == nil {
		s.agentPrepared = make(map[string]*preparedAgentReport)
	}
	for nodeID, prepared := range s.agentPrepared {
		if !prepared.inUse && now.Sub(prepared.admittedAt) > model.MaximumAgentSampleAge {
			delete(s.agentPrepared, nodeID)
		}
	}
	if prepared := s.agentPrepared[attempt.nodeID]; prepared != nil {
		if prepared.receipt != attempt.receipt {
			return nil, ErrAgentSampleConflict
		}
		prepared.inUse = true
		return prepared, nil
	}
	if len(s.agentPrepared) >= maximumPreparedAgentReports {
		return nil, ErrAgentIngestCapacity
	}
	prepared := &preparedAgentReport{receipt: attempt.receipt, admittedAt: now, inUse: true}
	s.agentPrepared[attempt.nodeID] = prepared
	return prepared, nil
}

func (s *MTSStore) removeAgentPlan(nodeID string, prepared *preparedAgentReport) {
	s.agentPreparedMu.Lock()
	defer s.agentPreparedMu.Unlock()
	if s.agentPrepared[nodeID] == prepared {
		delete(s.agentPrepared, nodeID)
	}
}

func (s *MTSStore) releaseAgentPlan(nodeID string, prepared *preparedAgentReport) {
	s.agentPreparedMu.Lock()
	defer s.agentPreparedMu.Unlock()
	prepared.inUse = false
	if !prepared.uncertain && s.agentPrepared[nodeID] == prepared {
		delete(s.agentPrepared, nodeID)
	}
}
