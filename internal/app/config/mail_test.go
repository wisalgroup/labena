package config

import (
	"strings"
	"testing"
)

func TestMailConfiguration(t *testing.T) {
	c := Defaults().Mail
	if err := c.Validate("MyApp"); err != nil {
		t.Fatal(err)
	}
	c.Username = "user"
	c.Password = "private-password"
	if err := c.Validate("MyApp"); err != nil {
		t.Fatal(err)
	}
	options := c.SMTPConfig("MyApp")
	if !options.UseTLS || options.InsecureSkipVerify {
		t.Fatal("TLS not verified")
	}
	c.Username = ""
	if err := c.Validate("MyApp"); err == nil || strings.Contains(err.Error(), c.Password) {
		t.Fatal("invalid credentials or unsafe error", err)
	}
	parsed, err := (environment{"MAIL_HOST": "sandbox.smtp.mailtrap.io", "MAIL_PORT": "2525", "MAIL_ACCENT_COLOR": "#7733AA", "MAIL_USERNAME": "user", "MAIL_PASSWORD": "password"}).decode()
	if err != nil || parsed.Mail.AccentColor != "#7733AA" || parsed.Mail.Port != 2525 {
		t.Fatal("mail settings not loaded", err)
	}
}

func TestMailBrandingFollowsAppNameAndAllowsOverrides(t *testing.T) {
	cfg, err := FromEnvironment(map[string]string{"APP_NAME": "MyApp"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mail.Brand(cfg.App.AppName).Name != "MyApp" || cfg.Mail.SMTPConfig(cfg.App.AppName).From.Name != "MyApp" || cfg.Mail.Brand(cfg.App.AppName).LogoURL != "" {
		t.Fatal("mail did not inherit application branding")
	}
	// Go callers can change the app name after Defaults without stale resolved names.
	cfg.App.AppName = "MyApp Support"
	if cfg.Mail.Brand(cfg.App.AppName).Name != cfg.App.AppName {
		t.Fatal("branding retained the old application name")
	}
	cfg.Mail.BrandName, cfg.Mail.FromName = "MyApp Accounts", "Support"
	if cfg.Mail.Brand(cfg.App.AppName).Name != "MyApp Accounts" || cfg.Mail.SMTPConfig(cfg.App.AppName).From.Name != "Support" {
		t.Fatal("explicit mail overrides were lost")
	}
	if _, err := FromEnvironment(map[string]string{"APP_NAME": " "}); err == nil {
		t.Fatal("blank application name accepted")
	}
}
