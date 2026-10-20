//go:build linux

package agentupdate_test

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/model"
)

func (f *transportFixture) ackHealth(t *testing.T, client *agentupdate.ControlClient, updater *agentupdate.Updater) {
	t.Helper()
	reporter, err := agent.NewHTTPReporter(agent.Config{ServerURL: f.server.URL, AgentToken: f.token, NodeName: f.node.Name, AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second})
	transportMust(t, err)
	var accepted time.Time
	var rejected atomic.Int64
	reporter.WithReportObserver(func(at time.Time) { accepted = at }, func(error) { rejected.Add(1) })
	f.rejectReport.Store(true)
	_, err = reporter.Report(t.Context(), transportSample("2.0.0"))
	if err == nil || !accepted.IsZero() {
		t.Fatal("incomplete ACK became health evidence")
	}
	_, targets, err := f.service.Detail(t.Context(), f.owner, f.rollout.ID)
	transportMust(t, err)
	if targets[0].State != "offered" {
		t.Fatal("rejected ACK advanced rollout", targets)
	}
	f.rejectReport.Store(false)
	for index := 0; index < 3; index++ {
		f.advance(time.Second)
		f.acceptAndObserve(t, healthTransport{reporter: reporter, client: client, updater: updater, accepted: &accepted, rejections: &rejected})
	}
	_, targets, err = f.service.Detail(t.Context(), f.owner, f.rollout.ID)
	transportMust(t, err)
	if targets[0].State != "observing" {
		t.Fatal("server window skipped", targets)
	}
	f.advance(31 * time.Second)
	f.acceptAndObserve(t, healthTransport{reporter: reporter, client: client, updater: updater, accepted: &accepted, rejections: &rejected})
}

type healthTransport struct {
	reporter   *agent.HTTPReporter
	client     *agentupdate.ControlClient
	updater    *agentupdate.Updater
	accepted   *time.Time
	rejections *atomic.Int64
}

func (f *transportFixture) acceptAndObserve(t *testing.T, transport healthTransport) {
	t.Helper()
	*transport.accepted = time.Time{}
	_, err := transport.reporter.Report(t.Context(), transportSample("2.0.0"))
	transportMust(t, err)
	if transport.accepted.IsZero() {
		t.Fatal("valid report was not acknowledged")
	}
	f.syncClock(*transport.accepted)
	_, err = transport.updater.Observe(t.Context(), agentupdate.HealthEvidence{ProcessID: "tls-restarted-fixture", Version: "2.0.0", ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now()})
	transportMust(t, err)
	observation, err := transport.updater.Observation(*transport.accepted)
	transportMust(t, err)
	if observation == nil {
		return
	}
	observation.ProtocolErrors = int(transport.rejections.Load())
	transportMust(t, transport.client.Observe(t.Context(), *observation))
}

func transportSample(version string) agent.ReportSample {
	return agent.ReportSample{SampleID: uuid.NewString(), CapturedAt: time.Now().UTC(), Metrics: model.NodeMetrics{CPU: 25, SystemInfo: model.SystemInfo{AgentVersion: version, OS: "linux", Arch: runtime.GOARCH}}}
}
