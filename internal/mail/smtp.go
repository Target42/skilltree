package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Sender sends plain-text mail via SMTP.
type Sender struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// TLSMode: starttls, tls, or none.
	TLSMode string
}

func (s *Sender) Send(to, subject, body string) error {
	if s == nil {
		return fmt.Errorf("mail sender not configured")
	}
	fromAddr, err := parseAddress(s.From)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	toAddr, err := parseAddress(to)
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}

	msg := buildMessage(s.From, toAddr, subject, body)
	addr := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))

	switch strings.ToLower(s.TLSMode) {
	case "tls":
		return s.sendTLS(addr, fromAddr, toAddr, msg)
	case "none":
		return s.sendPlain(addr, fromAddr, toAddr, msg)
	default: // starttls
		return s.sendStartTLS(addr, fromAddr, toAddr, msg)
	}
}

func (s *Sender) sendStartTLS(addr, from, to string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); !ok {
		return fmt.Errorf("smtp server does not support STARTTLS")
	}
	tlsCfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	if err := client.StartTLS(tlsCfg); err != nil {
		return err
	}
	if err := s.authenticate(client); err != nil {
		return err
	}
	return smtpData(client, from, to, msg)
}

func (s *Sender) sendTLS(addr, from, to string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := s.authenticate(client); err != nil {
		return err
	}
	return smtpData(client, from, to, msg)
}

func (s *Sender) sendPlain(addr, from, to string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := s.authenticate(client); err != nil {
		return err
	}
	return smtpData(client, from, to, msg)
}

func (s *Sender) authenticate(client *smtp.Client) error {
	if s.Username == "" {
		return nil
	}
	ok, _ := client.Extension("AUTH")
	if !ok {
		return fmt.Errorf("smtp server does not support AUTH")
	}
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	return client.Auth(auth)
}

func smtpData(client *smtp.Client, from, to string, msg []byte) error {
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildMessage(fromHeader, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + fromHeader + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + sanitizeHeader(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\r\n", "\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

func parseAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty address")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}

func sanitizeHeader(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, v)
}
