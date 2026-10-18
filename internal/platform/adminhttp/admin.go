// Package adminhttp protects browser-based operational endpoints independently
// of business authentication and CORS settings.
package adminhttp

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Config supplies a separate administrative account. Serve over HTTPS in production.
type Config struct {
	Realm    string
	Username string
	Password string `json:"-"`
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Username) == "" || strings.Contains(c.Username, ":") || strings.TrimSpace(c.Password) == "" || strings.ContainsFunc(c.Username, unicode.IsControl) || strings.ContainsFunc(c.Password, unicode.IsControl) {
		return errors.New("adminhttp: a username without colons and a password are required")
	}
	if strings.ContainsFunc(c.Realm, unicode.IsControl) {
		return errors.New("adminhttp: realm must not contain control characters")
	}
	return nil
}

// Protect applies Basic Auth, CSRF protection and same-origin read access.
// Native clients without browser-origin headers still require credentials.
// The wrapped handler may set a stricter Content-Security-Policy.
func Protect(next http.Handler, cfg Config) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("adminhttp: handler is required")
	}
	username, password := sha256.Sum256([]byte(cfg.Username)), sha256.Sum256([]byte(cfg.Password))
	challenge := "Basic realm=" + strconv.Quote(cfg.Realm) + ", charset=\"UTF-8\""
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		w.Header().Del("Access-Control-Allow-Origin")
		w.Header().Del("Access-Control-Allow-Credentials")
		if protection.Check(r) != nil || !sameOrigin(r) {
			http.Error(w, "Cross-origin access is not allowed", http.StatusForbidden)
			return
		}
		user, pass, ok := r.BasicAuth()
		userHash, passHash := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(pass))
		validUser := subtle.ConstantTimeCompare(userHash[:], username[:])
		validPassword := subtle.ConstantTimeCompare(passHash[:], password[:])
		if !ok || validUser&validPassword != 1 {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

// CSRF protection allows safe methods. Admin data also needs protection against
// cross-origin reads, including when an upstream layer mistakenly enables CORS.
func sameOrigin(r *http.Request) bool {
	if len(r.Header.Values("Origin")) > 1 || len(r.Header.Values("Sec-Fetch-Site")) > 1 {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		// Preserve the external Host through TLS termination; forwarded headers
		// never grant access. HTTPS browser origins can reach an internal HTTP hop.
		if err != nil || u.Host != r.Host || (u.Scheme != "https" && (u.Scheme != "http" || r.TLS != nil)) || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
			return false
		}
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "same-origin" || site == "none"
}
