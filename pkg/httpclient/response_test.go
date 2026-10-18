package httpclient_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/httpclient"
)

func TestDecodersRejectTruncatedBodies(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		decode     func(*httpclient.Response) error
	}{
		{"json", `{}`, func(r *httpclient.Response) error { var v any; return r.DecodeJSON(&v) }},
		{"xml", `<item/>`, func(r *httpclient.Response) error { var v struct{}; return r.DecodeXML(&v) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newClient(t, httpclient.Config{
				BaseURL: "https://example.com", MaxResponseBytes: int64(len(tc.body)),
				Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body + " extra"))}, nil
				}),
			})
			response, err := client.Do(context.Background(), getRequest{ID: "1"})
			if !errors.Is(err, httpclient.ErrResponseTooLarge) || response.Text() != tc.body {
				t.Fatalf("response = %+v, err = %v", response, err)
			}
			if !errors.Is(response.BodyError(), httpclient.ErrResponseTooLarge) || !errors.Is(tc.decode(response), httpclient.ErrResponseTooLarge) {
				t.Fatal("a valid prefix must not hide truncation")
			}
		})
	}
}

type failingResponseBody struct {
	io.Reader
	readErr, closeErr error
	closed            bool
}

func (b *failingResponseBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if errors.Is(err, io.EOF) && b.readErr != nil {
		return n, b.readErr
	}
	return n, err
}

func (b *failingResponseBody) Close() error { b.closed = true; return b.closeErr }

func TestResponseRetainsReadAndCloseFailures(t *testing.T) {
	closeErr := errors.New("close failed")
	body := &failingResponseBody{Reader: strings.NewReader(`{"id":123}`), readErr: io.ErrUnexpectedEOF, closeErr: closeErr}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com", Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body}, nil
	})})
	response, err := client.Do(context.Background(), getRequest{ID: "1"})
	if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, closeErr) || !body.closed {
		t.Fatalf("read error = %v, closed = %v", err, body.closed)
	}
	if !response.Successful() || response.CheckStatus() != nil {
		t.Fatal("HTTP status must stay separate from body errors")
	}
	value := struct{ ID int }{ID: 456}
	err = response.DecodeJSON(&value)
	var decode *httpclient.DecodeError
	if !errors.As(err, &decode) || !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, closeErr) || value.ID != 456 {
		t.Fatalf("decode = %+v, err = %v", value, err)
	}
	if !errors.Is(response.BodyError(), io.ErrUnexpectedEOF) || response.Text() != `{"id":123}` {
		t.Fatal("read failure or retained body lost")
	}
}

func TestJSONNumbersKeepTheirPrecision(t *testing.T) {
	response := &httpclient.Response{Body: []byte(`{"id":9007199254740993}`)}
	var dynamic map[string]any
	if err := response.DecodeJSON(&dynamic); err != nil {
		t.Fatal(err)
	}
	number, ok := dynamic["id"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("dynamic id = %v (%T)", dynamic["id"], dynamic["id"])
	}
	var typed struct{ ID int64 }
	if err := response.DecodeJSON(&typed); err != nil || typed.ID != 9007199254740993 {
		t.Fatalf("typed id = %d, err = %v", typed.ID, err)
	}
}

func TestDecodeErrorsRetainCausesWithoutExposingPayloads(t *testing.T) {
	response := &httpclient.Response{Body: []byte(`{"private_customer_field":"secret"}`)}
	var target map[string]int
	err := response.DecodeJSON(&target)
	var decode *httpclient.DecodeError
	var mismatch *json.UnmarshalTypeError
	if !errors.As(err, &decode) || decode.Format != "json" || !errors.As(err, &mismatch) {
		t.Fatalf("decode error = %v", err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
		t.Fatal("decode error disclosed provider data")
	}
	response.Body = []byte(`{"id":`)
	if err := response.DecodeJSON(&target); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("malformed JSON = %v", err)
	}
	for _, body := range []string{`{} {}`, `{} secret`} {
		response.Body = []byte(body)
		if err := response.DecodeJSON(&target); !errors.As(err, &decode) {
			t.Fatalf("trailing JSON data accepted: %q", body)
		}
	}
	response.Body = []byte(" \n\t")
	if err := response.DecodeJSON(&target); !errors.Is(err, io.EOF) {
		t.Fatalf("empty JSON = %v", err)
	}
	if err := response.DecodeXML(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("empty XML = %v", err)
	}
}

func TestXMLRejectsTrailingData(t *testing.T) {
	for _, tc := range []struct {
		name, suffix string
		valid        bool
	}{
		{"empty", "", true},
		{"whitespace", " \t\r\n", true},
		{"comment and processing instruction", `<!-- note --><?example data?>`, true},
		{"second root", `<item/>`, false},
		{"text", " secret", false},
		{"malformed", "<", false},
		{"declaration", `<?xml version="1.0"?>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &httpclient.Response{Body: []byte(`<item><name>Keyboard</name></item>` + tc.suffix)}
			var item struct {
				Name string `xml:"name"`
			}
			err := response.DecodeXML(&item)
			if tc.valid {
				if err != nil || item.Name != "Keyboard" {
					t.Fatalf("item = %+v, err = %v", item, err)
				}
			} else {
				var decode *httpclient.DecodeError
				if !errors.As(err, &decode) || decode.Format != "xml" {
					t.Fatalf("trailing data accepted: %v", err)
				}
			}
		})
	}
	response := &httpclient.Response{Body: []byte(`<item></wrong>`)}
	var syntax *xml.SyntaxError
	if err := response.DecodeXML(&struct{}{}); !errors.As(err, &syntax) {
		t.Fatalf("XML syntax cause lost: %v", err)
	}
}

func TestResponseContentType(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"Application/Problem+JSON; charset=utf-8", "application/problem+json"},
		{"text/plain", "text/plain"},
		{"", ""},
		{"application/json; charset=", ""},
	} {
		response := &httpclient.Response{Header: http.Header{"Content-Type": {tc.header}}}
		if got := response.ContentType(); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestResponseRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
		valid  bool
	}{
		{"0", 0, true},
		{" 120 ", 2 * time.Minute, true},
		{"", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"1.5", 0, false},
		{"9223372037", 0, false}, // Seconds overflow a time.Duration.
		{"18446744073709551616", 0, false},
		{"Thu, 01 Jan 1970 00:00:00 GMT", 0, true},
		{"Thu, 01 Jan 9999 00:00:00 GMT", 0, false},
	} {
		response := &httpclient.Response{Header: http.Header{"Retry-After": {tc.header}}}
		if got, ok := response.RetryAfter(); got != tc.want || ok != tc.valid {
			t.Errorf("%q: got (%v, %v), want (%v, %v)", tc.header, got, ok, tc.want, tc.valid)
		}
	}
	future := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	response := &httpclient.Response{Header: http.Header{"Retry-After": {future}}}
	if delay, ok := response.RetryAfter(); !ok || delay < 55*time.Second || delay > time.Minute {
		t.Fatalf("HTTP date delay = %v, valid = %v", delay, ok)
	}
}
