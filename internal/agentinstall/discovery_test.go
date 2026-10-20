package agentinstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

func (fixture *discoveryFixture) awaitPendingRequest(t *testing.T) store.EnrollmentRequest {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		page, err := fixture.enrollment.ListEnrollmentRequests(context.Background(), store.EnrollmentRequestFilter{})
		if err != nil {
			t.Fatalf("list requests: %v", err)
		}
		for _, request := range page.Requests {
			if request.Status == agentenrollment.RequestPending {
				return request
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending enrollment request appeared")
	return store.EnrollmentRequest{}
}

func (fixture *discoveryFixture) approve(t *testing.T, requestID, nodeName string) {
	t.Helper()
	if _, err := fixture.enrollment.ApproveEnrollmentRequest(context.Background(), store.EnrollmentApproval{
		RequestID: requestID, NodeName: nodeName, GroupID: fixture.groupID, SSHPort: 22,
		ActorID: "owner"}, time.Now()); err != nil {
		t.Fatalf("approve request: %v", err)
	}
}

func (fixture *discoveryFixture) requestCount(t *testing.T) int {
	t.Helper()
	page, err := fixture.enrollment.ListEnrollmentRequests(context.Background(), store.EnrollmentRequestFilter{})
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	return len(page.Requests)
}

func TestEnrollWithAutomaticGrantInstallsService(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	summary, err := fixture.installer.Enroll(context.Background(), fixture.options())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if summary.NodeID == "" || summary.ConfigPath != fixture.installer.paths.config {
		t.Fatalf("summary: %+v", summary)
	}
	config, err := agent.LoadConfig(fixture.installer.paths.config)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !strings.HasPrefix(config.AgentToken, "beat_agent_v1_") || config.SSHPort != 22 {
		t.Fatalf("unexpected agent config: %+v", config)
	}
	for _, name := range []string{"identity.json", grantStateFile} {
		assertMode(t, filepath.Join(fixture.installer.paths.stateDir, name), privateFile)
	}
	assertMode(t, fixture.installer.paths.config, privateFile)
	assertMode(t, fixture.installer.paths.binary, executableFile)
	for _, name := range []string{"identity.json", grantStateFile, "agent.json"} {
		content, err := os.ReadFile(filepath.Join(fixture.installer.paths.stateDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(content), fixture.secret.Plaintext) {
			t.Fatalf("grant secret persisted in %s", name)
		}
	}
	if fixture.requestCount(t) != 1 {
		t.Fatalf("expected exactly one enrollment request")
	}
}

func TestEnrollWaitsForManualApproval(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalManual)
	outcome := make(chan error, 1)
	go func() {
		_, err := fixture.installer.Enroll(context.Background(), fixture.options())
		outcome <- err
	}()
	fixture.approve(t, fixture.awaitPendingRequest(t).ID, "lab-1")
	select {
	case err := <-outcome:
		if err != nil {
			t.Fatalf("enroll after approval: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("enrollment did not finish after approval")
	}
	if _, err := agent.LoadConfig(fixture.installer.paths.config); err != nil {
		t.Fatalf("installed config is not readable: %v", err)
	}
}

func TestEnrollReportsRejection(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalManual)
	outcome := make(chan error, 1)
	go func() {
		_, err := fixture.installer.Enroll(context.Background(), fixture.options())
		outcome <- err
	}()
	request := fixture.awaitPendingRequest(t)
	if _, err := fixture.enrollment.RejectEnrollmentRequest(context.Background(), store.EnrollmentRejection{
		RequestID: request.ID, ActorID: "owner", Reason: "not needed"}, time.Now()); err != nil {
		t.Fatalf("reject: %v", err)
	}
	select {
	case err := <-outcome:
		if err == nil || !strings.Contains(err.Error(), "rejected") {
			t.Fatalf("rejection error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("enrollment did not stop after rejection")
	}
	if _, err := os.Stat(fixture.installer.paths.config); !os.IsNotExist(err) {
		t.Fatal("rejected enrollment wrote an agent config")
	}
}

func TestEnrollResumesRecordedRequest(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalManual)
	options := fixture.options()
	options.PollTimeout = 30 * time.Millisecond
	if _, err := fixture.installer.Enroll(context.Background(), options); err == nil {
		t.Fatal("expected pending enrollment to time out")
	}
	request := fixture.awaitPendingRequest(t)
	identityPath := filepath.Join(fixture.installer.paths.stateDir, "identity.json")
	identity, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatalf("read identity: %v", err)
	}
	fixture.approve(t, request.ID, "lab-1")
	options.PollTimeout = 5 * time.Second
	if _, err := fixture.installer.Enroll(context.Background(), options); err != nil {
		t.Fatalf("resume enrollment: %v", err)
	}
	if fixture.requestCount(t) != 1 {
		t.Fatalf("resume created a duplicate request: %d", fixture.requestCount(t))
	}
	after, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatalf("read identity: %v", err)
	}
	if string(after) != string(identity) {
		t.Fatal("resume generated a new enrollment key")
	}
}
