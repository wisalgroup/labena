package config

import "testing"

func TestTrustedProxyConfiguration(t *testing.T) {
	if len(Defaults().HTTP.TrustedProxies) != 0 {
		t.Fatal("forwarding headers trusted by default")
	}
	cfg, err := loadTestConfig(t, map[string]string{"HTTP_TRUSTED_PROXIES": "192.0.2.10/32,2001:db8::10/128"})
	if err != nil {
		t.Fatal(err)
	}
	prefixes, err := cfg.HTTP.TrustedProxyPrefixes()
	if err != nil || len(prefixes) != 2 {
		t.Fatal(prefixes, err)
	}
	for _, value := range []string{"*", "0.0.0.0/0", "::/0", "not-an-address", "192.0.2.10", "::ffff:192.0.2.10/128"} {
		if _, err := loadTestConfig(t, map[string]string{"HTTP_TRUSTED_PROXIES": value}); err == nil {
			t.Fatalf("accepted unsafe/invalid proxy setting %q", value)
		}
	}
}
