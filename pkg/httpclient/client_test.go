package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/httpclient"
)

type getRequest struct {
	httpclient.Request `method:"GET" path:"items/{id}"`
	ID                 string   `path:"id"`
	Tags               []string `query:"tag,omitempty"`
	Active             *bool    `query:"active,omitempty"`
	Language           string   `header:"Accept-Language,omitempty"`
}

func newClient(t *testing.T, cfg httpclient.Config) *httpclient.Client {
	t.Helper()
	client, err := httpclient.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func TestTaggedRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/items/a%2Fb%3Fc" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		if !reflect.DeepEqual(r.URL.Query()["tag"], []string{"sale & new", "second"}) || r.URL.Query().Get("active") != "false" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if r.Header.Get("Accept-Language") != "en" || r.Header.Get("X-Default") != "shared" {
			t.Errorf("headers = %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Trailer", "X-Checksum")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"name":"Keyboard"}`)
		w.Header().Set("X-Checksum", "complete")
	}))
	defer server.Close()
	client := newClient(t, httpclient.Config{BaseURL: server.URL + "/v1", Header: http.Header{"X-Default": {"shared"}, "Accept-Language": {"fr"}}})
	active := false
	response, err := client.Do(context.Background(), getRequest{ID: "a/b?c", Tags: []string{"sale & new", "second"}, Active: &active, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if err := response.CheckStatus(); err != nil {
		t.Fatal(err)
	}
	var result struct{ Name string }
	for range 2 {
		if err := response.DecodeJSON(&result); err != nil || result.Name != "Keyboard" {
			t.Fatalf("decode = %v, %v", result, err)
		}
	}
	if response.StatusCode != 201 || response.Trailer.Get("X-Checksum") != "complete" || response.Text() != `{"name":"Keyboard"}` {
		t.Fatalf("response = %+v", response)
	}
}

func TestStatusErrorDoesNotExposeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, "private provider response")
	}))
	defer server.Close()
	client := newClient(t, httpclient.Config{BaseURL: server.URL})
	response, err := client.Do(context.Background(), getRequest{ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var status *httpclient.StatusError
	if !errors.As(response.CheckStatus(), &status) || status.Response != response || strings.Contains(status.Error(), "private") {
		t.Fatal("status error lost response or disclosed body")
	}
}

type trackedBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error { b.closed.Store(true); return nil }

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBoundedAndStreamingResponses(t *testing.T) {
	var latest *trackedBody
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com", MaxResponseBytes: 4, Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		latest = &trackedBody{Reader: strings.NewReader("12345")}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: latest}, nil
	})})
	response, err := client.Do(context.Background(), getRequest{ID: "1"})
	if !errors.Is(err, httpclient.ErrResponseTooLarge) || response.Text() != "1234" || !latest.closed.Load() {
		t.Fatalf("limited response = %v, %v", response, err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.com/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := client.DoRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if latest.closed.Load() {
		t.Fatal("stream prematurely closed")
	}
	body, err := io.ReadAll(raw.Body)
	if err != nil || string(body) != "12345" {
		t.Fatalf("stream = %q, %v", body, err)
	}
	_ = raw.Body.Close()
	if !latest.closed.Load() {
		t.Fatal("caller could not close stream")
	}
}

func TestMiddlewareOrderAndIsolation(t *testing.T) {
	var order []string
	middleware := func(name string) httpclient.Middleware {
		return func(r *http.Request, next httpclient.Next) (*http.Response, error) {
			order = append(order, name+" before")
			r.Header.Set("X-Middleware", "set")
			response, err := next(r)
			order = append(order, name+" after")
			return response, err
		}
	}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com", Middleware: []httpclient.Middleware{middleware("a"), middleware("b")}, Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		order = append(order, "transport")
		if r.Header.Get("X-Middleware") != "set" {
			t.Error("middleware header missing")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})})
	request, err := client.Build(context.Background(), getRequest{ID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Fatal("Build ran middleware")
	}
	response, err := client.DoRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if request.Header.Get("X-Middleware") != "" {
		t.Fatal("middleware mutated caller request")
	}
	if want := []string{"a before", "b before", "transport", "b after", "a after"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v", order)
	}
	order = nil
	if _, err := client.Do(context.Background(), getRequest{ID: "1"}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 5 {
		t.Fatal("Do bypassed middleware")
	}
}

func TestShortCircuitClosesRequestBody(t *testing.T) {
	failure := errors.New("blocked")
	body := &trackedBody{Reader: strings.NewReader("payload")}
	client := newClient(t, httpclient.Config{Middleware: []httpclient.Middleware{func(r *http.Request, next httpclient.Next) (*http.Response, error) { return nil, failure }}})
	request, err := http.NewRequest(http.MethodPost, "https://example.com", body)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DoRequest(request)
	if !errors.Is(err, failure) || !body.closed.Load() {
		t.Fatalf("short circuit = %v, closed=%v", err, body.closed.Load())
	}
}

func TestConfigurationSnapshots(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/items/old" {
			close(entered)
			<-release
		}
		_, _ = io.WriteString(w, r.Header.Get("X-Version"))
	}))
	defer server.Close()
	defer releaseRequest()
	header := http.Header{"X-Version": {"old"}}
	client := newClient(t, httpclient.Config{BaseURL: server.URL, Header: header, MaxResponseBytes: 10})
	header.Set("X-Version", "caller mutation")
	result := make(chan error, 1)
	go func() {
		response, err := client.Do(context.Background(), getRequest{ID: "old"})
		if err == nil && response.Text() != "old" {
			err = errors.New("in-flight configuration changed")
		}
		result <- err
	}()
	select {
	case <-entered:
	case err := <-result:
		t.Fatalf("request ended before reaching provider: %v", err)
	case <-time.After(time.Second):
		t.Fatal("request did not reach provider")
	}
	cfg := client.Config()
	cfg.Header.Set("X-Version", "new")
	cfg.MaxResponseBytes = 1
	if err := client.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	releaseRequest()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), getRequest{ID: "new"})
	if !errors.Is(err, httpclient.ErrResponseTooLarge) || response.Text() != "n" {
		t.Fatalf("new config = %v,%v", response, err)
	}
	cfg.Header.Set("X-Version", "mutated")
	cfg.BaseURL = "https://example.com/?secret=invalid"
	if err := client.Configure(cfg); err == nil {
		t.Fatal("accepted invalid config")
	}
	if client.Config().Header.Get("X-Version") != "new" {
		t.Fatal("configuration not copied or failed update applied")
	}
}

func TestConcurrentConfigurationAndRequests(t *testing.T) {
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com", Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})})
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			for range 30 {
				if _, err := client.Do(context.Background(), getRequest{ID: "1"}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for range 30 {
		cfg := client.Config()
		cfg.Header.Set("X-Version", "new")
		if err := client.Configure(cfg); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestTimeoutIncludesResponseBodyAndRedirectsAreOff(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/items/redirect" {
			http.Redirect(w, r, "/target", 302)
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := newClient(t, httpclient.Config{BaseURL: server.URL, Timeout: 100 * time.Millisecond})
	response, err := client.Do(context.Background(), getRequest{ID: "redirect"})
	if err != nil || response.StatusCode != 302 || calls.Load() != 1 {
		t.Fatalf("redirect = %v,%v", response, err)
	}
	_, err = client.Do(context.Background(), getRequest{ID: "slow"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
}

type closeTransport struct{ closed atomic.Int32 }

func (t *closeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
}
func (t *closeTransport) CloseIdleConnections() { t.closed.Add(1) }

func TestMiddlewarePreservesConnectionCleanup(t *testing.T) {
	transport := &closeTransport{}
	client := newClient(t, httpclient.Config{Transport: transport, Middleware: []httpclient.Middleware{func(r *http.Request, next httpclient.Next) (*http.Response, error) { return next(r) }}})
	client.CloseIdleConnections()
	if transport.closed.Load() != 1 {
		t.Fatal("middleware hid connection cleanup")
	}
}

func TestInvalidConfigAndNativeDefaults(t *testing.T) {
	for _, cfg := range []httpclient.Config{
		{BaseURL: "ftp://example.com"}, {BaseURL: "https://user:secret@example.com"},
		{BaseURL: "https://example.com?key=secret"}, {BaseURL: "https://example.com#fragment"},
		{Timeout: -time.Second}, {MaxResponseBytes: -1}, {Middleware: []httpclient.Middleware{nil}},
		{Transport: (*http.Transport)(nil)}, {Header: http.Header{"X-Test": {"bad\r\nvalue"}}},
		{Header: http.Header{"Content-Length": {"5"}}},
	} {
		if _, err := httpclient.New(cfg); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	client := newClient(t, httpclient.Config{Timeout: -1, Header: http.Header{"Authorization": {"Bearer example"}}, Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("native request inherited tagged-request defaults")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})})
	request, err := http.NewRequest(http.MethodGet, "https://other.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.DoRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}
