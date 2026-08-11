package notification

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestDeliverEmailPlaintextLoopback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	received := make(chan string, 1)
	go serveSMTPOnce(t, listener, received)

	port := listener.Addr().(*net.TCPAddr).Port
	config := EmailConfig{
		Host: "localhost", Port: port, Username: "beat", Password: "secret",
		From: "beat@example.com", To: []string{"ops@example.com"}, Security: EmailSecurityNone,
	}
	if err := deliverEmail(context.Background(), config, "Beat test", "hello"); err != nil {
		t.Fatalf("deliverEmail() error = %v", err)
	}
	select {
	case message := <-received:
		if !strings.Contains(message, "Subject: Beat test") || !strings.Contains(message, "hello") {
			t.Fatalf("message = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP server did not receive message")
	}
}

func serveSMTPOnce(t *testing.T, listener net.Listener, received chan<- string) {
	t.Helper()
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	reader := textproto.NewReader(bufio.NewReader(connection))
	writer := bufio.NewWriter(connection)
	writeSMTPLine(t, writer, "220 localhost ESMTP")
	for {
		line, readErr := reader.ReadLine()
		if readErr != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"):
			writeSMTPResponse(t, writer, "250-localhost\r\n250 AUTH PLAIN")
		case strings.HasPrefix(line, "AUTH PLAIN"):
			writeSMTPLine(t, writer, "235 authenticated")
		case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
			writeSMTPLine(t, writer, "250 OK")
		case line == "DATA":
			writeSMTPLine(t, writer, "354 End data")
			message, dotErr := reader.ReadDotBytes()
			if dotErr != nil {
				t.Errorf("read message: %v", dotErr)
				return
			}
			received <- string(message)
			writeSMTPLine(t, writer, "250 queued")
		case line == "QUIT":
			writeSMTPLine(t, writer, "221 bye")
			return
		default:
			t.Errorf("unexpected SMTP command: %s", line)
			return
		}
	}
}

func TestDeliverEmailErrors(t *testing.T) {
	t.Run("connection", func(t *testing.T) {
		config := EmailConfig{Host: "127.0.0.1", Port: 1, Security: EmailSecurityTLS}
		if err := deliverEmail(t.Context(), config, "subject", "body"); err == nil {
			t.Fatal("deliverEmail() error = nil")
		}
	})

	t.Run("missing STARTTLS", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go serveSMTPWithoutExtensions(listener)
		config := EmailConfig{
			Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
			Security: EmailSecuritySTARTTLS,
		}
		if err := deliverEmail(t.Context(), config, "subject", "body"); err == nil {
			t.Fatal("deliverEmail() error = nil")
		}
	})

	t.Run("mail rejected", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go serveSMTPRejectMail(listener)
		config := EmailConfig{
			Host: "localhost", Port: listener.Addr().(*net.TCPAddr).Port,
			From: "beat@example.com", To: []string{"ops@example.com"}, Security: EmailSecurityNone,
		}
		if err := deliverEmail(t.Context(), config, "subject", "body"); err == nil {
			t.Fatal("deliverEmail() error = nil")
		}
	})
}

func TestBuildEmailMessageValidation(t *testing.T) {
	if _, err := buildEmailMessage(EmailConfig{From: "bad"}, "subject", "body"); err == nil {
		t.Fatal("invalid sender error = nil")
	}
	if _, err := buildEmailMessage(
		EmailConfig{From: "beat@example.com", To: []string{"bad"}},
		"subject",
		"body",
	); err == nil {
		t.Fatal("invalid recipient error = nil")
	}
	if config := tlsConfig("smtp.example.com"); config.ServerName != "smtp.example.com" {
		t.Fatalf("TLS config = %#v", config)
	}
}

func TestDeliverEmailProtocolFailures(t *testing.T) {
	for _, command := range []string{"AUTH", "RCPT", "DATA", "QUIT"} {
		t.Run(command, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			go serveSMTPRejectCommand(listener, command)
			config := EmailConfig{Host: "localhost", Port: listener.Addr().(*net.TCPAddr).Port,
				Username: "beat", Password: "secret", From: "beat@example.com",
				To: []string{"ops@example.com"}, Security: EmailSecurityNone}
			if err := deliverEmail(t.Context(), config, "subject", "body"); err == nil {
				t.Fatalf("SMTP %s rejection was ignored", command)
			}
		})
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for invalid message: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go serveSMTPRejectCommand(listener, "")
	config := EmailConfig{Host: "localhost", Port: listener.Addr().(*net.TCPAddr).Port,
		From: "bad", To: []string{"ops@example.com"}, Security: EmailSecurityNone}
	if err := deliverEmail(t.Context(), config, "subject", "body"); err == nil {
		t.Fatal("invalid email message was delivered")
	}
}

func serveSMTPRejectCommand(listener net.Listener, rejected string) {
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	reader := textproto.NewReader(bufio.NewReader(connection))
	writer := bufio.NewWriter(connection)
	_, _ = writer.WriteString("220 localhost ESMTP\r\n")
	_ = writer.Flush()
	for {
		line, readErr := reader.ReadLine()
		if readErr != nil {
			return
		}
		command := strings.SplitN(line, " ", 2)[0]
		if command == rejected {
			_, _ = writer.WriteString("550 rejected\r\n")
			_ = writer.Flush()
			return
		}
		switch command {
		case "EHLO":
			_, _ = writer.WriteString("250-localhost\r\n250 AUTH PLAIN\r\n")
		case "AUTH":
			_, _ = writer.WriteString("235 authenticated\r\n")
		case "MAIL", "RCPT":
			_, _ = writer.WriteString("250 OK\r\n")
		case "DATA":
			_, _ = writer.WriteString("354 End data\r\n")
			_ = writer.Flush()
			_, _ = reader.ReadDotBytes()
			_, _ = writer.WriteString("250 queued\r\n")
		case "QUIT":
			_, _ = writer.WriteString("221 bye\r\n")
			_ = writer.Flush()
			return
		}
		_ = writer.Flush()
	}
}

func serveSMTPWithoutExtensions(listener net.Listener) {
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	_, _ = writer.WriteString("220 localhost ESMTP\r\n")
	_ = writer.Flush()
	_, _ = reader.ReadString('\n')
	_, _ = writer.WriteString("250 localhost\r\n")
	_ = writer.Flush()
}

func serveSMTPRejectMail(listener net.Listener) {
	connection, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	_, _ = writer.WriteString("220 localhost ESMTP\r\n")
	_ = writer.Flush()
	_, _ = reader.ReadString('\n')
	_, _ = writer.WriteString("250 localhost\r\n")
	_ = writer.Flush()
	_, _ = reader.ReadString('\n')
	_, _ = writer.WriteString("550 sender rejected\r\n")
	_ = writer.Flush()
}

func writeSMTPLine(t *testing.T, writer *bufio.Writer, line string) {
	t.Helper()
	if _, err := fmt.Fprintf(writer, "%s\r\n", line); err != nil {
		t.Errorf("write SMTP response: %v", err)
		return
	}
	if err := writer.Flush(); err != nil {
		t.Errorf("flush SMTP response: %v", err)
	}
}

func writeSMTPResponse(t *testing.T, writer *bufio.Writer, response string) {
	t.Helper()
	if _, err := writer.WriteString(response + "\r\n"); err != nil {
		t.Errorf("write SMTP response: %v", err)
		return
	}
	if err := writer.Flush(); err != nil {
		t.Errorf("flush SMTP response: %v", err)
	}
}
