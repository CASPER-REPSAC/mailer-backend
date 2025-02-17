package email

import (
	"fmt"
	"net/smtp"

	"github.com/jordan-wright/email"
)

// SMTPConfig holds the configuration for the SMTP server.
type SMTPConfig struct {
	Host     string // SMTP server host, e.g., "smtp.example.com"
	Port     int    // SMTP server port, e.g., 587 (STARTTLS) 또는 465 (TLS)
	Username string // SMTP 계정의 사용자 이름
	Password string // SMTP 계정의 비밀번호
	From     string // 기본 발신자 이메일 주소 (예: "Your Name <your@example.com>")
}

// SMTPClient is a client for sending emails via SMTP using a third-party library.
type SMTPClient struct {
	Config SMTPConfig
}

// NewSMTPClient creates a new SMTPClient with the provided configuration.
func NewSMTPClient(cfg SMTPConfig) *SMTPClient {
	return &SMTPClient{
		Config: cfg,
	}
}

// SendEmail sends an email with the given recipients, subject, and HTML body using the "github.com/jordan-wright/email" library.
func (c *SMTPClient) SendEmail(to []string, subject, body string) error {
	e := email.NewEmail()
	e.From = c.Config.From
	e.To = to
	e.Subject = subject
	e.HTML = []byte(body)

	addr := fmt.Sprintf("%s:%d", c.Config.Host, c.Config.Port)
	auth := smtp.PlainAuth("", c.Config.Username, c.Config.Password, c.Config.Host)
	return e.Send(addr, auth)
}
