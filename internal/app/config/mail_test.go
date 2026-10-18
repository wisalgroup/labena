package config

import (
	"strings"
	"testing"
)

func TestMailConfiguration(t *testing.T) {
	c := Defaults().Mail
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Username = "user"
	c.Password = "private-password"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	options := c.SMTPConfig()
	if !options.UseTLS || options.InsecureSkipVerify {
		t.Fatal("TLS not verified")
	}
	c.Username = ""
	if err := c.Validate(); err == nil || strings.Contains(err.Error(), c.Password) {
		t.Fatal("invalid credentials or unsafe error", err)
	}
	parsed, err := (environment{"MAIL_HOST": "sandbox.smtp.mailtrap.io", "MAIL_PORT": "2525", "MAIL_ACCENT_COLOR": "#7733AA", "MAIL_USERNAME": "user", "MAIL_PASSWORD": "password"}).decode()
	if err != nil || parsed.Mail.AccentColor != "#7733AA" || parsed.Mail.Port != 2525 {
		t.Fatal("mail settings not loaded", err)
	}
}
