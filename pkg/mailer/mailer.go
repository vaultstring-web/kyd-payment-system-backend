package mailer

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
}

type Mailer struct {
	cfg Config
}

func New(cfg Config) *Mailer {
	return &Mailer{cfg: cfg}
}

func (m *Mailer) Send(to, subject, body string) error {
	from := m.cfg.From
	if strings.TrimSpace(from) == "" {
		from = m.cfg.Username
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)

	msg := buildMessage(from, to, subject, body)
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)

	if m.cfg.UseTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			ServerName: m.cfg.Host,
			MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, m.cfg.Host)
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

