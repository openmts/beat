package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

const smtpTimeout = 15 * time.Second

func deliverEmail(ctx context.Context, config EmailConfig, subject, body string) error {
	client, err := openSMTPClient(ctx, config)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err := authenticateSMTP(client, config); err != nil {
		return err
	}
	message, err := buildEmailMessage(config, subject, body)
	if err != nil {
		return err
	}
	if err := writeSMTPMessage(client, config, message); err != nil {
		return err
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP session: %w", err)
	}
	return nil
}

func openSMTPClient(ctx context.Context, config EmailConfig) (*smtp.Client, error) {
	address := net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port))
	connection, err := dialSMTP(ctx, address, config)
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(connection, config.Host)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("create SMTP client: %w", err)
	}
	if config.Security == EmailSecuritySTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig(config.Host)); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	return client, nil
}

func dialSMTP(ctx context.Context, address string, config EmailConfig) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: smtpTimeout}
	var connection net.Conn
	var err error
	if config.Security == EmailSecurityTLS {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsConfig(config.Host)}
		connection, err = tlsDialer.DialContext(ctx, "tcp", address)
	} else {
		connection, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("connect SMTP server: %w", err)
	}
	deadline := time.Now().Add(smtpTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("set SMTP deadline: %w", err)
	}
	return connection, nil
}

func tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

func authenticateSMTP(client *smtp.Client, config EmailConfig) error {
	if config.Username == "" {
		return nil
	}
	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("authenticate SMTP: %w", err)
	}
	return nil
}

func buildEmailMessage(config EmailConfig, subject, body string) ([]byte, error) {
	from, err := mail.ParseAddress(config.From)
	if err != nil {
		return nil, fmt.Errorf("parse sender address: %w", err)
	}
	recipients := make([]string, 0, len(config.To))
	for _, raw := range config.To {
		recipient, parseErr := mail.ParseAddress(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("parse recipient address: %w", parseErr)
		}
		recipients = append(recipients, recipient.String())
	}
	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", from.String())
	fmt.Fprintf(&message, "To: %s\r\n", strings.Join(recipients, ", "))
	fmt.Fprintf(&message, "Subject: %s\r\n", subject)
	message.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	message.WriteString(body)
	return message.Bytes(), nil
}

func writeSMTPMessage(client *smtp.Client, config EmailConfig, message []byte) error {
	if err := client.Mail(config.From); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	for _, recipient := range config.To {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("set SMTP recipient: %w", err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	return nil
}
