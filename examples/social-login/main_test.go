package main

import (
	"strings"
	"testing"

	"golang.org/x/oauth2/google"
)

func TestGoogleConfigWithoutRedirectURIs(t *testing.T) {
	for _, secret := range []string{"", "test-secret"} {
		input := `{"installed":{"client_id":"test.apps.googleusercontent.com","client_secret":"` + secret + `","auth_uri":"https://untrusted.example/auth","token_uri":"https://untrusted.example/token"}}`
		config, err := googleConfig([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if config.ClientID != "test.apps.googleusercontent.com" || config.ClientSecret != secret || config.Endpoint != google.Endpoint || config.RedirectURL != "" {
			t.Fatal("credentials or fixed endpoints changed")
		}
	}
}

func TestGoogleConfigRejectsInvalidExportsWithoutLeakingContents(t *testing.T) {
	for _, input := range []string{`invalid-test-secret`, `null`, `{}`, `{"installed":null}`, `{"web":{"client_id":"test.apps.googleusercontent.com"}}`, `{"installed":{"client_id":"invalid","client_secret":"test-secret"}}`} {
		_, err := googleConfig([]byte(input))
		if err == nil {
			t.Fatal("invalid export accepted")
		}
		if strings.Contains(err.Error(), "test-secret") {
			t.Fatal("secret in error")
		}
	}
}
