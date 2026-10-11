package mailer

import (
	"fmt"
	"net/smtp"
	"strings"

	"webhooklens/internal/config"
)

type Mailer interface {
	SendVerificationEmail(toEmail, firstName, token string) error
}

type SMTPMailer struct {
	cfg *config.Config
}

func New(cfg *config.Config) *SMTPMailer {
	return &SMTPMailer{cfg: cfg}
}

func (m *SMTPMailer) SendVerificationEmail(toEmail, firstName, token string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, m.cfg.SMTPPort)
	verifyURL := fmt.Sprintf("%s/api/auth/verify-email?token=%s", m.cfg.AppBaseURL, token)

	subject := "Verify your WebHookLens account"
	recipientName := strings.TrimSpace(firstName)
	if recipientName == "" {
		recipientName = "Developer"
	}

	body := fmt.Sprintf(
		"From: WebHookLens <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/html; charset=UTF-8\r\n"+
			"\r\n"+
			"<!DOCTYPE html><html><body style=\"font-family: Arial, sans-serif; line-height: 1.6; color: #333;\">"+
			"<h2>Welcome to WebHookLens, %s!</h2>"+
			"<p>Thank you for creating an account. Please click the link below to verify your email address:</p>"+
			"<p><a href=\"%s\" style=\"background-color: #2563eb; color: #fff; padding: 10px 20px; text-decoration: none; border-radius: 5px; display: inline-block;\">Verify Email Address</a></p>"+
			"<p>Or copy and paste this link in your browser:</p>"+
			"<p style=\"color: #666; font-size: 14px;\">%s</p>"+
			"<p>This verification link will expire in 24 hours.</p>"+
			"<p>Best regards,<br>The WebHookLens Team</p>"+
			"</body></html>\r\n",
		m.cfg.SMTPFrom,
		toEmail,
		subject,
		recipientName,
		verifyURL,
		verifyURL,
	)

	return smtp.SendMail(addr, nil, m.cfg.SMTPFrom, []string{toEmail}, []byte(body))
}
