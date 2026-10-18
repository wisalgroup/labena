// Package httpclient sends declarative, tagged requests through net/http.
// Clients are safe for concurrent use; request values and body streams belong
// to their callers and must not be mutated while a call is using them.
package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

// Next sends a request through the remaining middleware. Call it synchronously.
type Next func(*http.Request) (*http.Response, error)

// Middleware surrounds one HTTP exchange. It may edit the provided request's
// headers: the client has already cloned them. Bodies remain single-use streams.
// Middleware must be concurrency-safe and normally call next exactly once.
type Middleware func(request *http.Request, next Next) (*http.Response, error)

// Config is a complete client configuration. New and Configure copy Header and
// Middleware; transports, cookie jars and function closures are borrowed and
// must be safe for concurrent use. Do not mutate a transport after using it.
type Config struct {
	BaseURL          string
	Header           http.Header
	Timeout          time.Duration // Zero selects 30s; -1 disables the client timeout.
	MaxResponseBytes int64         // Zero selects 10 MiB; applies only to Do.
	Middleware       []Middleware
	Transport        http.RoundTripper
	CheckRedirect    func(*http.Request, []*http.Request) error
	Jar              http.CookieJar
}

// Client reuses connections and publishes configuration changes atomically.
// Construct it with New. Each call captures one configuration snapshot.
// The native client stays private: exposing mutable fields or promoting its
// Get/Post methods would bypass these configuration and tagged-request contracts.
type Client struct {
	state atomic.Pointer[clientState]
	base  http.RoundTripper
}

type clientState struct {
	cfg    Config
	url    *url.URL
	client *http.Client
}

// New validates configuration without sending requests or starting workers.
func New(cfg Config) (*Client, error) {
	base := http.DefaultTransport
	if transport, ok := base.(*http.Transport); ok {
		base = transport.Clone()
	}
	c := &Client{base: base}
	if err := c.Configure(cfg); err != nil {
		return nil, err
	}
	return c, nil
}

// Config returns a snapshot with independent header and middleware containers.
func (c *Client) Config() Config { return cloneConfig(c.state.Load().cfg) }

// Configure replaces the complete configuration. Invalid changes leave the old
// snapshot intact. In-flight requests retain their original settings. Custom
// transports are borrowed: their owner retires them after their users finish.
func (c *Client) Configure(cfg Config) error {
	cfg = cloneConfig(cfg)
	if cfg.Transport != nil && isNil(reflect.ValueOf(cfg.Transport)) || cfg.Jar != nil && isNil(reflect.ValueOf(cfg.Jar)) {
		return errors.New("httpclient: transport and cookie jar must not contain typed nil pointers")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = 10 << 20
	}
	if cfg.Timeout < -1 || cfg.MaxResponseBytes < 1 || cfg.MaxResponseBytes == 1<<63-1 {
		return errors.New("httpclient: invalid timeout or response limit")
	}
	var baseURL *url.URL
	if cfg.BaseURL != "" {
		var err error
		baseURL, err = url.Parse(cfg.BaseURL)
		if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" || baseURL.Opaque != "" {
			return errors.New("httpclient: BaseURL must be an HTTP(S) URL without credentials, query or fragment")
		}
		if !strings.HasSuffix(baseURL.EscapedPath(), "/") {
			baseURL.Path += "/"
			if baseURL.RawPath != "" {
				baseURL.RawPath += "/"
			}
		}
	}
	var err error
	cfg.Header, err = cleanHeader(cfg.Header)
	if err != nil {
		return err
	}
	transport := cfg.Transport
	if transport == nil {
		transport = c.base
	}
	if transport == nil || isNil(reflect.ValueOf(transport)) {
		return errors.New("httpclient: nil default transport")
	}
	for _, middleware := range cfg.Middleware {
		if middleware == nil {
			return errors.New("httpclient: nil middleware")
		}
	}
	if len(cfg.Middleware) > 0 {
		transport = newMiddlewareTransport(transport, cfg.Middleware)
	}
	timeout := cfg.Timeout
	if timeout == -1 {
		timeout = 0
	}
	redirect := cfg.CheckRedirect
	if redirect == nil {
		redirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	c.state.Store(&clientState{cfg: cfg, url: baseURL, client: &http.Client{
		Transport: transport, Timeout: timeout, CheckRedirect: redirect, Jar: cfg.Jar,
	}})
	return nil
}

// Build constructs a native request without running middleware or sending it.
// After success, the caller owns Body until handing it to DoRequest (or another
// HTTP client). Close Body when abandoning a built request, including multipart.
func (c *Client) Build(ctx context.Context, input any) (*http.Request, error) {
	return buildRequest(ctx, c.state.Load(), input)
}

// Do builds, sends and buffers a tagged request. It closes the response body.
// Non-2xx statuses are responses, not execution errors; call CheckStatus explicitly.
// A body read error or limit violation returns both the partial response and error.
func (c *Client) Do(ctx context.Context, input any) (*Response, error) {
	snapshot := c.state.Load()
	request, err := buildRequest(ctx, snapshot, input)
	if err != nil {
		return nil, err
	}
	result, err := snapshot.client.Do(request)
	if err != nil {
		return nil, err
	}
	return readResponse(result, snapshot.cfg.MaxResponseBytes)
}

// DoRequest sends a native request through the same middleware, timeout, cookie
// and redirect policies. Supply a complete URL and headers; BaseURL and default
// Header are not applied. The caller must close the returned response body.
func (c *Client) DoRequest(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("httpclient: nil request")
	}
	return c.state.Load().client.Do(request.Clone(request.Context()))
}

// CloseIdleConnections releases idle connections, including the client's default
// pool. It does not interrupt active calls or make the client unusable. A custom
// transport may share a pool with other clients, as with net/http itself.
func (c *Client) CloseIdleConnections() {
	c.state.Load().client.CloseIdleConnections()
	if closer, ok := c.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func cloneConfig(cfg Config) Config {
	cfg.Header = cfg.Header.Clone()
	if cfg.Header == nil {
		cfg.Header = make(http.Header)
	}
	cfg.Middleware = append([]Middleware(nil), cfg.Middleware...)
	return cfg
}

type middlewareTransport struct {
	base http.RoundTripper
	next Next
}

func newMiddlewareTransport(base http.RoundTripper, middleware []Middleware) *middlewareTransport {
	next := Next(base.RoundTrip)
	for i := len(middleware) - 1; i >= 0; i-- {
		current, following := middleware[i], next
		next = func(request *http.Request) (*http.Response, error) {
			forwarded := false
			defer func() {
				// Short-circuiting middleware still owns the request body contract.
				if !forwarded && request.Body != nil {
					_ = request.Body.Close()
				}
			}()
			return current(request, func(request *http.Request) (*http.Response, error) {
				forwarded = true
				return following(request)
			})
		}
	}
	return &middlewareTransport{base: base, next: next}
}

func (t *middlewareTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	return t.next(request)
}

func (t *middlewareTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func cleanHeader(input http.Header) (http.Header, error) {
	header := make(http.Header, len(input))
	for name, values := range input {
		if !validToken(name) {
			return nil, errors.New("httpclient: invalid header name")
		}
		switch strings.ToLower(name) {
		case "host", "content-length", "transfer-encoding", "trailer":
			return nil, fmt.Errorf("httpclient: configure %s through a native request", name)
		}
		for _, value := range values {
			if !validHeaderValue(value) {
				return nil, fmt.Errorf("httpclient: invalid value for header %s", name)
			}
			header.Add(name, value)
		}
	}
	return header, nil
}

func validToken(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c > 127 || !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	for _, c := range value {
		if c == 127 || c < 32 && c != '\t' {
			return false
		}
	}
	return true
}

// closeBody is also used when request construction fails after acquiring a body.
func closeBody(body io.Reader) {
	if closer, ok := body.(io.Closer); ok {
		_ = closer.Close()
	}
}
