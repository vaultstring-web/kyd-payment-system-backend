package mailer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Config holds the configuration for the mailer.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool

	// Gmail API configuration
	GmailAPIEnabled      bool
	GmailCredentialsPath string
	GmailTokenPath       string
}

// Sender defines the interface for sending emails.
type Sender interface {
	Send(to, subject, body string) error
}

// Mailer manages email sending using either SMTP or Gmail API.
type Mailer struct {
	cfg    Config
	sender Sender
}

// New creates a new Mailer instance.
func New(cfg Config) (*Mailer, error) {
	var sender Sender
	var err error

	if cfg.GmailAPIEnabled {
		sender, err = NewGmailSender(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gmail sender: %w", err)
		}
	} else {
		sender = &SMTPSender{cfg: cfg}
	}

	return &Mailer{
		cfg:    cfg,
		sender: sender,
	}, nil
}

// WithSender allows overriding the default sender (useful for testing).
func (m *Mailer) WithSender(sender Sender) *Mailer {
	m.sender = sender
	return m
}

// Send sends an email to the specified recipient.
func (m *Mailer) Send(to, subject, body string) error {
	return m.sender.Send(to, subject, body)
}

// NoopSender implements the Sender interface without sending anything.
type NoopSender struct{}

func (s *NoopSender) Send(to, subject, body string) error {
	fmt.Printf("\n[MAILER NOOP] to: %s, subject: %s\n", to, subject)
	return nil
}

// SMTPSender implements the Sender interface using SMTP.
type SMTPSender struct {
	cfg Config
}

func (s *SMTPSender) Send(to, subject, body string) error {
	from := s.cfg.From
	if strings.TrimSpace(from) == "" {
		from = s.cfg.Username
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	msg := buildMessage(from, to, subject, body)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	if s.cfg.UseTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			ServerName: s.cfg.Host,
			MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			return err
		}
		defer c.Quit()
		if err := c.Auth(auth); err != nil {
			return err
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(msg)); err != nil {
			return err
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// GmailSender implements the Sender interface using Gmail API.
type GmailSender struct {
	cfg     Config
	service *gmail.Service
}

// NewGmailSender creates a new GmailSender instance.
func NewGmailSender(cfg Config) (*GmailSender, error) {
	ctx := context.Background()

	// Use service account credentials if available
	var opts []option.ClientOption
	if cfg.GmailCredentialsPath != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.GmailCredentialsPath))
	}

	// Create Gmail service
	service, err := gmail.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return &GmailSender{
		cfg:     cfg,
		service: service,
	}, nil
}

func (s *GmailSender) Send(to, subject, body string) error {
	from := s.cfg.From
	if strings.TrimSpace(from) == "" {
		from = "me" // Default to authenticated user
	}

	header := make(map[string]string)
	header["From"] = from
	header["To"] = to
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/html; charset=\"UTF-8\""

	var msg string
	for k, v := range header {
		msg += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	msg += "\r\n" + body

	gMsg := &gmail.Message{
		Raw: base64.URLEncoding.EncodeToString([]byte(msg)),
	}

	_, err := s.service.Users.Messages.Send("me", gMsg).Do()
	return err
}

func buildMessage(from, to, subject, body string) string {
	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=\"UTF-8\"",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body
}
