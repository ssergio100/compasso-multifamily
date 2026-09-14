package accountmail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/ssergio100/compasso/server/web"
)

type SMTP struct {
	address  string
	host     string
	username string
	password string
	from     string
	timeout  time.Duration
}

func NewSMTP(address, username, password, from string) (*SMTP, error) {
	address = strings.TrimSpace(address)
	from = strings.TrimSpace(from)
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, errors.New("SMTP address must contain host and port")
	}
	parsedFrom, err := mail.ParseAddress(from)
	if err != nil || parsedFrom.Address != from || strings.ContainsAny(from, "\r\n") {
		return nil, errors.New("SMTP sender must be one plain e-mail address")
	}
	if (username == "") != (password == "") {
		return nil, errors.New("SMTP username and password must be configured together")
	}
	return &SMTP{
		address: address, host: host, username: username, password: password,
		from: from, timeout: 15 * time.Second,
	}, nil
}

func (*SMTP) Available() bool { return true }

func (m *SMTP) SendAccountMessage(ctx context.Context, message web.AccountMessage) error {
	destination, err := mail.ParseAddress(strings.TrimSpace(message.To))
	if err != nil || destination.Address != strings.TrimSpace(message.To) ||
		strings.ContainsAny(message.Subject, "\r\n") {
		return errors.New("invalid account e-mail message")
	}
	dialer := &net.Dialer{Timeout: m.timeout}
	connection, err := dialer.DialContext(ctx, "tcp", m.address)
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(m.timeout))
	client, err := smtp.NewClient(connection, m.host)
	if err != nil {
		return fmt.Errorf("start SMTP: %w", err)
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("SMTP server does not offer STARTTLS")
	}
	if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: m.host}); err != nil {
		return fmt.Errorf("secure SMTP: %w", err)
	}
	if m.username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(m.from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(destination.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	payload := "From: " + m.from + "\r\n" +
		"To: " + destination.Address + "\r\n" +
		"Subject: " + message.Subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" + message.Text + "\r\n"
	if _, err := body.Write([]byte(payload)); err != nil {
		_ = body.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("send SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	return nil
}
