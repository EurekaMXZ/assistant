package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host      string
	Port      int
	Security  string
	Username  string
	Password  string
	FromEmail string
	FromName  string
}

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, config SMTPConfig, message Message) error
}

type SMTPSender struct {
	Timeout time.Duration
}

func (s SMTPSender) Send(ctx context.Context, config SMTPConfig, message Message) error {
	if err := validateMessage(config, message); err != nil {
		return err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	address := net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port))
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}
	if config.Security == SecurityTLS {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("establish SMTP TLS: %w", err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, config.Host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()
	if config.Security == SecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return fmt.Errorf("authenticate to SMTP server: %w", err)
		}
	}
	if err := client.Mail(config.FromEmail); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	recipient, _ := stdmail.ParseAddress(message.To)
	if err := client.Rcpt(recipient.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	if err := writeMessage(writer, config, message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("send SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close SMTP session: %w", err)
	}
	return nil
}

func validateMessage(config SMTPConfig, message Message) error {
	settings := Settings{Enabled: true, Host: config.Host, Port: config.Port, Security: config.Security, Username: config.Username, FromEmail: config.FromEmail, FromName: config.FromName}
	if err := ValidateSettings(settings); err != nil {
		return err
	}
	if strings.ContainsAny(message.To+message.Subject, "\r\n") {
		return errors.New("mail message contains invalid header characters")
	}
	address, err := stdmail.ParseAddress(strings.TrimSpace(message.To))
	if err != nil || address.Address == "" {
		return errors.New("mail recipient is invalid")
	}
	if strings.TrimSpace(message.Subject) == "" {
		return errors.New("mail subject is required")
	}
	return nil
}

func writeMessage(writer io.Writer, config SMTPConfig, message Message) error {
	buffered := bufio.NewWriter(writer)
	from := (&stdmail.Address{Name: config.FromName, Address: config.FromEmail}).String()
	recipient, _ := stdmail.ParseAddress(message.To)
	headers := []string{
		"From: " + from,
		"To: " + recipient.String(),
		"Subject: " + message.Subject,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	if _, err := buffered.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.ReplaceAll(message.Body, "\n", "\r\n")); err != nil {
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("flush SMTP message: %w", err)
	}
	return nil
}
