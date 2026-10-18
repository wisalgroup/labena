package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/mailview"
)

// MailConfig owns SMTP delivery and public email branding. Credentials are never
// passed into templates. The app always constructs delivery; SMTP connects only on Send.
type MailConfig struct {
	Host         string `env:"MAIL_HOST" envDefault:"sandbox.smtp.mailtrap.io"`
	Port         int    `env:"MAIL_PORT" envDefault:"2525"`
	Username     string `env:"MAIL_USERNAME" json:"-"`
	Password     string `env:"MAIL_PASSWORD" json:"-"`
	FromAddress  string `env:"MAIL_FROM_ADDRESS" envDefault:"no-reply@example.com"`
	FromName     string `env:"MAIL_FROM_NAME" envDefault:"Labena"`
	BrandName    string `env:"MAIL_BRAND_NAME" envDefault:"Labena"`
	AccentColor  string `env:"MAIL_ACCENT_COLOR" envDefault:"#1D4ED8"`
	LogoURL      string `env:"MAIL_LOGO_URL" envDefault:"https://placehold.co/80x80/172554/FFFFFF.png?text=L"`
	WebsiteURL   string `env:"MAIL_WEBSITE_URL" envDefault:"https://example.com"`
	SupportEmail string `env:"MAIL_SUPPORT_EMAIL" envDefault:"support@example.com"`
	Footer       string `env:"MAIL_FOOTER" envDefault:"Account updates, thoughtfully delivered."`

	// MaxAttachmentBytes is a Go-only override; zero uses mail.DefaultMaxAttachmentBytes.
	MaxAttachmentBytes int64 `env:"-"`
}

func (c MailConfig) Brand() mailview.Brand {
	return mailview.Brand{Name: c.BrandName, AccentColor: c.AccentColor, LogoURL: c.LogoURL, WebsiteURL: c.WebsiteURL, SupportEmail: c.SupportEmail, Footer: c.Footer}
}

func (c MailConfig) Validate() error {
	if err := c.Brand().Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.FromAddress) == "" {
		return errors.New("config: MAIL_FROM_ADDRESS is required")
	}
	if err := c.SMTPConfig().Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// SMTPConfig requires STARTTLS with certificate verification on every connection.
func (c MailConfig) SMTPConfig() mail.SMTPConfig {
	return mail.SMTPConfig{Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, From: mail.Address{Email: c.FromAddress, Name: c.FromName}, UseTLS: true, MaxAttachmentBytes: c.MaxAttachmentBytes}
}
