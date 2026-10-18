package config

import (
	"testing"

	"github.com/wisalgroup/labena/internal/platform/mail"
	"github.com/wisalgroup/labena/pkg/cache"
)

func TestSelectedCacheValidationMatchesOwner(t *testing.T) {
	for _, driver := range []cache.Driver{cache.DriverMemory, cache.DriverRedis, cache.DriverDatabase} {
		for _, change := range []struct {
			name  string
			apply func(*CacheConfig)
		}{
			{"defaults", func(*CacheConfig) {}},
			{"namespace", func(c *CacheConfig) { c.Namespace = "bad:namespace" }},
			{"table", func(c *CacheConfig) { c.DatabaseTable = "bad table" }},
			{"size", func(c *CacheConfig) { c.MaxEntryBytes = -1 }},
			{"zero-defaults", func(c *CacheConfig) { c.MemoryMaxBytes = 0; c.OperationTimeout = 0; c.Namespace = "" }},
		} {
			t.Run(string(driver)+"/"+change.name, func(t *testing.T) {
				cfg := Defaults().Cache
				cfg.Driver = driver
				change.apply(&cfg)
				var ownerErr error
				switch driver {
				case cache.DriverMemory:
					ownerErr = cfg.MemoryConfig().Validate()
				case cache.DriverRedis:
					ownerErr = cfg.RedisConfig().Validate()
				case cache.DriverDatabase:
					ownerErr = cfg.DatabaseConfig().Validate()
				}
				if (cfg.Validate() == nil) != (ownerErr == nil) {
					t.Fatal("app/owner validation mismatch", ownerErr)
				}
			})
		}
	}
	if err := (CacheConfig{Driver: cache.DriverDisabled}).Validate(); err != nil {
		t.Fatal("disabled cache requires settings", err)
	}
}

func TestSMTPValidationMatchesConstruction(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*MailConfig)
		valid bool
	}{
		{"defaults", func(*MailConfig) {}, true},
		{"host", func(c *MailConfig) { c.Host = " " }, false},
		{"port", func(c *MailConfig) { c.Port = 65536 }, false},
		{"credentials", func(c *MailConfig) { c.Password = "private" }, false},
		{"sender", func(c *MailConfig) { c.FromAddress = "invalid" }, false},
		{"name", func(c *MailConfig) { c.FromName = "bad\r\nname" }, false},
		{"bound", func(c *MailConfig) { c.MaxAttachmentBytes = -1 }, false},
		{"override", func(c *MailConfig) { c.MaxAttachmentBytes = 1024 }, true},
	} {
		t.Run(change.name, func(t *testing.T) {
			cfg := Defaults().Mail
			change.apply(&cfg)
			_, err := mail.NewSMTP(cfg.SMTPConfig())
			if (cfg.Validate() == nil) != change.valid || (err == nil) != change.valid {
				t.Fatal("validation mismatch", err)
			}
		})
	}
	options := Defaults().Mail.SMTPConfig()
	options.From = mail.Address{}
	if _, err := mail.NewSMTP(options); err != nil {
		t.Fatal("standalone message-level From override lost", err)
	}
}

func TestNamedFilesystemSelectionIsPure(t *testing.T) {
	root := t.TempDir() + "/unopened"
	cfg := FilesystemConfig{Default: "disabled", Disks: map[string]*DiskConfig{
		"local": {Driver: "local", Root: root}, "nil": nil, "invalid": {Driver: "s3"},
	}}
	before := *cfg.Disks["local"]
	options, err := cfg.StorageConfig("local")
	if err != nil || options.Root != root || options.Driver != "local" {
		t.Fatal(options, err)
	}
	if *cfg.Disks["local"] != before || cfg.Default != "disabled" {
		t.Fatal("configuration mutated")
	}
	for _, name := range []string{"missing", "nil", "invalid"} {
		if _, err := cfg.StorageConfig(name); err == nil {
			t.Fatal("accepted", name)
		}
	}
}
