package email

import (
	"fmt"
	"net/smtp"
	"time"

	"github.com/jordan-wright/email"
)

// SMTPConfig holds the configuration for the SMTP server.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// SMTPClient is a client for sending emails via SMTP using a third-party library.
type SMTPClient struct {
	Config SMTPConfig
	Pool   *email.Pool
}

// NewSMTPClient creates a new SMTPClient with the provided configuration.
func NewSMTPClient(cfg SMTPConfig) *SMTPClient {
	client := SMTPClient{
		Config: cfg,
	}
	pool, err := email.NewPool(fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), 10, smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host))
	if err != nil {
		panic(fmt.Errorf("failed to create email pool: %w", err))
	}
	client.Pool = pool
	return &client
}

// SendEmail sends an email with the given recipients, subject, and HTML body using the "github.com/jordan-wright/email" library.
func (c *SMTPClient) SendEmail(to []string, subject, body string, attachments []email.Attachment) error {
	e := email.NewEmail()
	e.From = c.Config.From
	e.To = to
	e.Subject = subject
	e.HTML = []byte(body)
	for _, attachment := range attachments {
		e.Attachments = append(e.Attachments, &attachment)
	}
	return c.Pool.Send(e, 10*time.Second)
}
