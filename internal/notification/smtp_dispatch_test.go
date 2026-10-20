package notification

import (
	"encoding/json"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func smtpDispatchFixture(t *testing.T, rejected string) (*Dispatcher, model.NotificationDispatch) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go serveSMTPRejectCommand(listener, rejected)
	port := listener.Addr().(*net.TCPAddr).Port
	config, err := json.Marshal(ProviderConfig{Host: "localhost", Port: port, Username: "beat", Password: "PRIVATE_PASSWORD", From: "sender@example.test", To: []string{"PRIVATE_RECIPIENT@example.test"}, Security: "none"})
	if err != nil {
		t.Fatal(err)
	}
	policy := model.NotificationEgressPolicy{Exceptions: []model.NotificationEgressException{{Host: "localhost", Port: port, Purpose: "local SMTP stub", ExpiresAt: time.Now().Add(time.Hour)}}}
	return NewDispatcher(), model.NotificationDispatch{DeliveryID: "email", ChannelType: TypeEmail, Config: config, Payload: model.NotificationPayload{Subject: "Subject", Text: "Body"}, Policy: policy}
}

func TestSMTPDispatcherUsesControlledConnectionAndSanitizedStages(t *testing.T) {
	for _, rejected := range []string{"", "AUTH", "MAIL", "RCPT", "DATA", "QUIT"} {
		t.Run("stage_"+rejected, func(t *testing.T) {
			dispatcher, dispatch := smtpDispatchFixture(t, rejected)
			result := dispatcher.Send(t.Context(), dispatch)
			wantSuccess := rejected == "" || rejected == "QUIT"
			if result.Success != wantSuccess {
				t.Fatalf("SMTP stage %s: %#v", rejected, result)
			}
			encoded, err := json.Marshal(result)
			if err != nil || strings.Contains(string(encoded), "PRIVATE_") {
				t.Fatal("SMTP leaked recipient/credential")
			}
		})
	}
	for _, code := range []int{450, 550} {
		result := smtpFailure(&textproto.Error{Code: code, Msg: "PRIVATE_REMOTE_BODY"}, "recipient")
		if result.Retryable != (code == 450) || strings.Contains(result.ErrorCode, "PRIVATE") {
			t.Fatal("SMTP classification unsafe")
		}
	}
}
