package notification

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type secureSMTPStub struct {
	connection net.Conn
	protocol   *textproto.Conn
	tls        *tls.Config
	encrypted  bool
	rejectData bool
	received   chan string
}

func (stub *secureSMTPStub) serve() error {
	if err := stub.protocol.PrintfLine("220 smtp.example ESMTP"); err != nil {
		return err
	}
	for {
		line, err := stub.protocol.ReadLine()
		if err != nil {
			return err
		}
		if err := stub.command(line); err != nil {
			return err
		}
	}
}

func (stub *secureSMTPStub) command(line string) error {
	command := strings.SplitN(line, " ", 2)[0]
	switch command {
	case "EHLO":
		return stub.protocol.PrintfLine("250-smtp.example\r\n250-STARTTLS\r\n250 AUTH PLAIN")
	case "STARTTLS":
		if err := stub.protocol.PrintfLine("220 ready for TLS"); err != nil {
			return err
		}
		return stub.upgrade()
	case "AUTH":
		if !stub.encrypted {
			return errors.New("credentials attempted before TLS")
		}
		return stub.protocol.PrintfLine("235 authenticated")
	case "MAIL", "RCPT":
		if strings.Contains(line, "Display") || !strings.Contains(line, "@example.test>") {
			return stub.protocol.PrintfLine("550 invalid envelope mailbox")
		}
		return stub.protocol.PrintfLine("250 accepted")
	case "DATA":
		return stub.data()
	case "QUIT":
		if err := stub.protocol.PrintfLine("221 bye"); err != nil {
			return err
		}
		return io.EOF
	default:
		return errors.New("unexpected SMTP fixture command")
	}
}

func (stub *secureSMTPStub) upgrade() error {
	secured := tls.Server(stub.connection, stub.tls)
	if err := secured.Handshake(); err != nil {
		return err
	}
	stub.connection, stub.protocol, stub.encrypted = secured, textproto.NewConn(secured), true
	return nil
}

func (stub *secureSMTPStub) data() error {
	if err := stub.protocol.PrintfLine("354 send body"); err != nil {
		return err
	}
	body, err := stub.protocol.ReadDotBytes()
	if err != nil {
		return err
	}
	stub.received <- string(body)
	if stub.rejectData {
		return stub.protocol.PrintfLine("451 PRIVATE_REJECTION")
	}
	return stub.protocol.PrintfLine("250 queued")
}

func tlsSMTPFixture(t *testing.T, security string, rejectData bool) (*Dispatcher, model.NotificationDispatch, <-chan string) {
	t.Helper()
	serverTLS, clientTLS := notificationTestTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	received, done := make(chan string, 1), make(chan struct{})
	go runSecureSMTP(listener, secureSMTPOptions{tls: serverTLS, implicit: security == "tls", rejectData: rejectData, received: received, done: done})
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SMTP fixture did not terminate")
		}
	})
	egress := NewEgress()
	egress.Resolver = resolvingStub{addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}
	egress.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}
	config, err := json.Marshal(ProviderConfig{Host: "smtp.example", Port: 465, Username: "beat", Password: "PRIVATE_PASSWORD", From: "Sender Display <sender@example.test>", To: []string{"Recipient Display <recipient@example.test>"}, Security: security})
	if err != nil {
		t.Fatal(err)
	}
	return NewDispatcher(DispatcherConfig{Egress: egress, TLSConfig: clientTLS}), testDispatch(TypeEmail, config), received
}

type secureSMTPOptions struct {
	tls                  *tls.Config
	implicit, rejectData bool
	received             chan string
	done                 chan struct{}
}

func runSecureSMTP(listener net.Listener, options secureSMTPOptions) {
	defer close(options.done)
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return
	}
	stub := &secureSMTPStub{connection: connection, protocol: textproto.NewConn(connection), tls: options.tls, rejectData: options.rejectData, received: options.received}
	if options.implicit {
		if err := stub.upgrade(); err != nil {
			return
		}
	}
	_ = stub.serve()
}

func TestSMTPUsesVerifiedTLSAndCanonicalEnvelopeMailboxes(t *testing.T) {
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			dispatcher, dispatch, received := tlsSMTPFixture(t, security, false)
			if result := dispatcher.Send(t.Context(), dispatch); !result.Success {
				t.Fatalf("verified SMTP delivery failed: %#v", result)
			}
			select {
			case body := <-received:
				if !strings.Contains(body, "Message") || !strings.Contains(body, "Subject:") {
					t.Fatal("SMTP receiver lost the bounded UTF-8 message")
				}
			default:
				t.Fatal("SMTP success was returned before DATA acceptance")
			}
		})
	}
}

func TestSMTPRejectsUntrustedCertificateAndRetriesTemporaryDataRejection(t *testing.T) {
	dispatcher, dispatch, _ := tlsSMTPFixture(t, "tls", false)
	dispatcher.tls.RootCAs = x509.NewCertPool()
	if result := dispatcher.Send(t.Context(), dispatch); result.Success || result.Stage != "tls" {
		t.Fatal("SMTP ignored certificate verification")
	}
	dispatcher, dispatch, _ = tlsSMTPFixture(t, "starttls", true)
	if result := dispatcher.Send(t.Context(), dispatch); result.Success || !result.Retryable || result.Stage != "data" || result.StatusClass != "4xx" {
		t.Fatal("SMTP DATA rejection was not classified as retryable")
	}
}

func TestSMTPEnvelopeRejectsInvalidAddressAndStripsDisplayNames(t *testing.T) {
	for _, config := range []ProviderConfig{{From: "bad"}, {From: "sender@example.test", To: []string{"bad"}}} {
		if _, err := smtpEnvelope(config); err == nil {
			t.Fatal("invalid envelope address accepted")
		}
	}
}
