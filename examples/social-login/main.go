// This local-only example tests an installed-app Google OAuth client with the API.
// Browser/mobile applications should use their supported Google SDK instead.
package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

func main() {
	credentials := flag.String("credentials", "", "Path to installed-app Google client JSON")
	api := flag.String("api", "http://localhost:8080/api/v1", "API base URL")
	output := flag.String("output", ".env.google-session.json", "New private file for the API token response")
	flag.Parse()
	if err := run(*credentials, *api, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(credentials, api, output string) error {
	u, err := url.Parse(api)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid API URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return errors.New("API requires HTTPS except on loopback")
	}
	data, err := os.ReadFile(credentials)
	if err != nil {
		return errors.New("cannot read Google credentials file")
	}
	config, err := googleConfig(data)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("output must be a new writable file; choose another -output path")
	}
	saved := false
	defer func() {
		file.Close()
		if !saved {
			os.Remove(output)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(path string, body any) (json.RawMessage, error) {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(api, "/")+path, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return nil, errors.New("API request failed")
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("API returned HTTP %d; check configuration, account eligibility and API logs", res.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
		if err != nil {
			return nil, err
		}
		if !json.Valid(raw) {
			return nil, errors.New("invalid API response")
		}
		return raw, nil
	}
	nonce := oauth2.GenerateVerifier()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	config.RedirectURL = "http://" + listener.Addr().String() + "/callback"
	state, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	type result struct {
		code string
		err  error
	}
	received := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid state", 400)
			return
		}
		item := result{code: r.URL.Query().Get("code")}
		if r.URL.Query().Get("error") != "" || item.code == "" {
			item.err = errors.New("Google sign-in was cancelled or denied")
		}
		select {
		case received <- item:
		default:
		}
		fmt.Fprintln(w, "Return to the terminal to see the sign-in result. You can close this tab.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case received <- result{err: errors.New("local callback server failed")}:
			default:
			}
		}
	}()
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if server.Shutdown(closeCtx) != nil {
			server.Close()
		}
		<-done
	}()
	fmt.Println("Open this URL in your browser:")
	fmt.Println(config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce)))
	var callback result
	select {
	case callback = <-received:
	case <-ctx.Done():
		return errors.New("sign-in timed out; start again")
	}
	if callback.err != nil {
		return callback.err
	}
	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, client)
	googleToken, err := config.Exchange(exchangeCtx, callback.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return errors.New("Google code exchange failed; use a Desktop OAuth client export, including its client_secret when supplied, and check consent settings")
	}
	idToken, _ := googleToken.Extra("id_token").(string)
	if idToken == "" {
		return errors.New("Google did not return an ID token")
	}
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
	payload, verifyErr := idtoken.Validate(verifyCtx, idToken, config.ClientID)
	verifyCancel()
	if verifyErr != nil {
		return errors.New("Google ID token validation failed")
	}
	actualNonce, _ := payload.Claims["nonce"].(string)
	if subtle.ConstantTimeCompare([]byte(actualNonce), []byte(nonce)) != 1 {
		return errors.New("Google nonce validation failed")
	}
	response, err := post("/auth/social/google", map[string]string{"id_token": idToken, "platform": "web", "device_name": "Local Google test"})
	if err != nil {
		return err
	}
	if _, err = file.Write(response); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	saved = true
	fmt.Println("Signed in. API credentials saved to", output, "(owner-only permissions).")
	return nil
}

// The callback is allocated at runtime, so redirect_uris from the export is unused.
// Only client credentials are read; endpoints always come from Google's library.
func googleConfig(data []byte) (*oauth2.Config, error) {
	var document struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	if json.Unmarshal(data, &document) != nil || document.Installed == nil {
		return nil, errors.New("expected a Google installed-app credentials JSON object")
	}
	c := document.Installed
	if !strings.HasSuffix(c.ClientID, ".apps.googleusercontent.com") || strings.ContainsAny(c.ClientID, " \t\r\n") {
		return nil, errors.New("Google credentials must contain a valid client_id")
	}
	return &oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: google.Endpoint, Scopes: []string{"openid", "email", "profile"}}, nil
}
