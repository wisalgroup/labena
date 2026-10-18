package integrations

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/modules/identity/domain"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

type keyTransport func(*http.Request) (*http.Response, error)

func (f keyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGoogleVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := func(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.Bytes()) }
	jwks, _ := json.Marshal(map[string]any{"keys": []any{map[string]string{"kid": "test", "kty": "RSA", "alg": "RS256", "use": "sig", "n": encoded(key.N), "e": encoded(big.NewInt(int64(key.E)))}}})
	client := &http.Client{Transport: keyTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": []string{"max-age=3600"}}, Body: io.NopCloser(strings.NewReader(string(jwks))), Request: r}, nil
	})}
	validator, err := idtoken.NewValidator(t.Context(), option.WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGoogle("test-client")
	if err != nil {
		t.Fatal(err)
	}
	g.validator = validator
	for _, tc := range []struct {
		name              string
		changes           jwt.MapClaims
		trustedPresenters []string
		valid             bool
		emailVerified     bool
	}{
		{name: "gmail", valid: true, emailVerified: true},
		{name: "workspace", changes: jwt.MapClaims{"email": "person@example.com", "hd": "example.com"}, valid: true, emailVerified: true},
		{name: "third party ownership not assumed", changes: jwt.MapClaims{"email": "person@example.com"}, valid: true},
		{name: "unverified", changes: jwt.MapClaims{"email_verified": false}, valid: true},
		{name: "wrong audience", changes: jwt.MapClaims{"aud": "other-client"}},
		{name: "wrong issuer", changes: jwt.MapClaims{"iss": "https://attacker.example"}},
		{name: "expired", changes: jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}},
		{name: "future issued", changes: jwt.MapClaims{"iat": time.Now().Add(time.Hour).Unix()}},
		{name: "nonce belongs to client flow", changes: jwt.MapClaims{"nonce": "other"}, valid: true, emailVerified: true},
		{name: "no API nonce required", changes: jwt.MapClaims{"nonce": nil}, valid: true, emailVerified: true},
		{name: "missing subject", changes: jwt.MapClaims{"sub": ""}},
		{name: "wrong presenter", changes: jwt.MapClaims{"azp": "other-client"}},
		{name: "audience presenter", changes: jwt.MapClaims{"azp": "test-client"}, valid: true, emailVerified: true},
		{name: "trusted Android presenter", changes: jwt.MapClaims{"azp": "android-client"}, trustedPresenters: []string{"android-client", "ios-client"}, valid: true, emailVerified: true},
		{name: "second trusted presenter", changes: jwt.MapClaims{"azp": "ios-client"}, trustedPresenters: []string{"android-client", "ios-client"}, valid: true, emailVerified: true},
		{name: "audience presenter with additional clients", changes: jwt.MapClaims{"azp": "test-client"}, trustedPresenters: []string{"android-client"}, valid: true, emailVerified: true},
		{name: "missing presenter with additional clients", trustedPresenters: []string{"android-client"}, valid: true, emailVerified: true},
		{name: "untrusted presenter with additional clients", changes: jwt.MapClaims{"azp": "other-client"}, trustedPresenters: []string{"android-client"}},
		{name: "trusted presenter cannot bypass audience", changes: jwt.MapClaims{"aud": "other-client", "azp": "android-client"}, trustedPresenters: []string{"android-client"}},
		{name: "presenter is not an additional audience", changes: jwt.MapClaims{"aud": "android-client", "azp": "android-client"}, trustedPresenters: []string{"android-client"}},
		{name: "empty presenter", changes: jwt.MapClaims{"azp": ""}},
		{name: "null presenter", changes: jwt.MapClaims{"azp": nil}},
		{name: "numeric presenter", changes: jwt.MapClaims{"azp": 42}},
		{name: "array presenter", changes: jwt.MapClaims{"azp": []string{"android-client"}}, trustedPresenters: []string{"android-client"}},
		{name: "object presenter", changes: jwt.MapClaims{"azp": map[string]string{"client_id": "android-client"}}, trustedPresenters: []string{"android-client"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier, err := NewGoogle("test-client", tc.trustedPresenters...)
			if err != nil {
				t.Fatal(err)
			}
			verifier.validator = validator
			claims := jwt.MapClaims{"iss": "https://accounts.google.com", "aud": "test-client", "sub": "stable-subject", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": "challenge-nonce", "email": "person@gmail.com", "email_verified": true, "name": "Person"}
			for k, v := range tc.changes {
				claims[k] = v
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "test"
			raw, err := token.SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := verifier.Verify(t.Context(), raw)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.valid && (identity.Subject != "stable-subject" || identity.EmailVerified != tc.emailVerified) {
				t.Fatal(identity)
			}
			if !tc.valid && !errors.Is(err, domain.ErrSocialInvalid) {
				t.Fatalf("expected invalid social credential, got %v", err)
			}
		})
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	forged := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"aud": "test-client", "exp": time.Now().Add(time.Hour).Unix()})
	forged.Header["kid"] = "test"
	forgedRaw, err := forged.SignedString(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Verify(t.Context(), forgedRaw); err == nil {
		t.Fatal("forged signature accepted")
	}
	if _, err := g.Verify(context.Background(), "not-a-token"); err == nil {
		t.Fatal("malformed accepted")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"aud": "test-client"})
	raw, _ := token.SignedString([]byte("attacker"))
	if _, err := g.Verify(t.Context(), raw); err == nil {
		t.Fatal("untrusted algorithm accepted")
	}
}
