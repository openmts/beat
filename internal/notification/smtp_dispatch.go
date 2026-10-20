package notification

import (
	"context"
	"crypto/tls"
	"errors"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"

	"github.com/beat/backend/internal/model"
)

type smtpDispatch struct {
	config  ProviderConfig
	payload model.NotificationPayload
	policy  model.NotificationEgressPolicy
}

func (dispatcher *Dispatcher) sendSMTP(ctx context.Context, input smtpDispatch) model.NotificationSendResult {
	config := input.config
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	connection, err := dispatcher.egress.DialChecked(ctx, DialTarget{Network: "tcp", Address: address, Policy: input.policy, LoopbackOnly: config.Security == EmailSecurityNone})
	if err != nil {
		return transportResult(err)
	}
	defer func() { _ = connection.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return smtpFailure(err, "connect")
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer func() { _ = stop() }()
	secured, err := dispatcher.secureSMTP(ctx, connection, config)
	if err != nil {
		return smtpFailure(err, "tls")
	}
	client, err := smtp.NewClient(secured, config.Host)
	if err != nil {
		return smtpFailure(err, "greeting")
	}
	defer func() { _ = client.Close() }()
	if stage, err := dispatcher.authenticateProviderSMTP(client, config); err != nil {
		return smtpFailure(err, stage)
	}
	return sendSMTPMessage(client, input)
}

func (dispatcher *Dispatcher) secureSMTP(ctx context.Context, connection net.Conn, config ProviderConfig) (net.Conn, error) {
	if config.Security != EmailSecurityTLS {
		return connection, nil
	}
	secured := tls.Client(connection, dispatcher.smtpTLS(config.Host))
	if err := secured.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return secured, nil
}

func (dispatcher *Dispatcher) smtpTLS(host string) *tls.Config {
	config := dispatcher.tls.Clone()
	config.ServerName = host
	return config
}

func (dispatcher *Dispatcher) authenticateProviderSMTP(client *smtp.Client, config ProviderConfig) (string, error) {
	if config.Security == EmailSecuritySTARTTLS {
		if err := client.StartTLS(dispatcher.smtpTLS(config.Host)); err != nil {
			return "tls", err
		}
	}
	if config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return "auth", err
		}
	}
	return "", nil
}

func sendSMTPMessage(client *smtp.Client, input smtpDispatch) model.NotificationSendResult {
	config := input.config
	email := EmailConfig{Host: config.Host, Port: config.Port, Username: config.Username, Password: config.Password, From: config.From, To: config.To, Security: config.Security}
	body, err := buildEmailMessage(email, mime.QEncoding.Encode("utf-8", input.payload.Subject), input.payload.Text)
	if err != nil {
		return sendRejected("message_invalid", "render")
	}
	if len(body) > maximumRequestBytes {
		return sendRejected("message_too_large", "render")
	}
	envelope, err := smtpEnvelope(config)
	if err != nil {
		return sendRejected("message_invalid", "render")
	}
	if err := client.Mail(envelope.From); err != nil {
		return smtpFailure(err, "envelope")
	}
	for _, recipient := range envelope.To {
		if err := client.Rcpt(recipient); err != nil {
			return smtpFailure(err, "recipient")
		}
	}
	return writeDispatchSMTPData(client, body)
}

func smtpEnvelope(config ProviderConfig) (EmailConfig, error) {
	envelope := EmailConfig{To: []string{}}
	sender, err := mail.ParseAddress(config.From)
	if err != nil {
		return envelope, ErrProviderConfig
	}
	envelope.From = sender.Address
	for _, raw := range config.To {
		recipient, err := mail.ParseAddress(raw)
		if err != nil {
			return envelope, ErrProviderConfig
		}
		envelope.To = append(envelope.To, recipient.Address)
	}
	return envelope, nil
}

func writeDispatchSMTPData(client *smtp.Client, body []byte) model.NotificationSendResult {
	writer, err := client.Data()
	if err != nil {
		return smtpFailure(err, "data")
	}
	if _, err := writer.Write(body); err != nil {
		_ = writer.Close()
		return smtpFailure(err, "data")
	}
	if err := writer.Close(); err != nil {
		return smtpFailure(err, "data")
	}
	_ = client.Quit()
	return model.NotificationSendResult{Success: true, StatusClass: "2xx"}
}

func smtpFailure(err error, stage string) model.NotificationSendResult {
	var response *textproto.Error
	if errors.As(err, &response) {
		return model.NotificationSendResult{Retryable: response.Code >= 400 && response.Code < 500, ErrorCode: "smtp_rejected", Stage: stage, StatusClass: strconv.Itoa(response.Code/100) + "xx"}
	}
	result := transportResult(err)
	result.Stage = stage
	return result
}
