package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func clientResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestExpoStandaloneSendAndReceipts(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "exp.host" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("bad endpoint/auth")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(r.URL.Path, "/send") {
			if body["to"] != "ExpoPushToken[phone]" || body["expiration"] == nil || body["sound"] != "default" {
				t.Fatal(body)
			}
			return clientResponse(200, `{"data":{"status":"ok","id":"ticket-1"}}`), nil
		}
		if body["ids"].([]any)[0] != "ticket-1" {
			t.Fatal(body)
		}
		return clientResponse(200, `{"data":{"ticket-1":{"status":"ok"}}}`), nil
	})}
	sender, err := NewExpo(ExpoConfig{AccessToken: "secret", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour)
	ticket, err := sender.Send(t.Context(), "ExpoPushToken[phone]", Message{Title: "Ready", ExpiresAt: &expiry})
	if err != nil || ticket.ID != "ticket-1" {
		t.Fatal(ticket, err)
	}
	receipt, err := sender.Receipt(t.Context(), ticket.ID)
	if err != nil || !receipt.Found || receipt.Error != nil {
		t.Fatal(receipt, err)
	}
	expiry = time.Now().Add(-time.Second)
	_, err = sender.Send(t.Context(), "ExpoPushToken[phone]", Message{Title: "Expired", ExpiresAt: &expiry})
	if err == nil || calls != 2 {
		t.Fatal(err, calls)
	}
}
func TestExpoErrorsAreClassifiedAndPrivate(t *testing.T) {
	for _, test := range []struct {
		name, body, code   string
		status             int
		permanent, invalid bool
	}{
		{"invalid destination", `{"data":{"status":"error","message":"private registration","details":{"error":"DeviceNotRegistered"}}}`, "unregistered", 200, true, true},
		{"throttle", `private details`, "rate_limited", 429, false, false},
		{"credentials", `private details`, "credentials", 401, true, false},
		{"unavailable", `private details`, "unavailable", 503, false, false},
		{"missing ticket", `{"data":{"status":"ok"}}`, "invalid_response", 200, false, false},
		{"malformed", `{`, "invalid_response", 200, false, false},
		{"top level", `{"errors":[{"code":"UNAUTHORIZED","message":"private details"}]}`, "credentials", 200, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender, _ := NewExpo(ExpoConfig{HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				r := clientResponse(test.status, test.body)
				r.Header.Set("Retry-After", "120")
				return r, nil
			})}})
			_, err := sender.Send(t.Context(), "ExponentPushToken[phone]", Message{Title: "Test"})
			var failure *Error
			if !errors.As(err, &failure) || failure.Code != test.code || failure.Permanent != test.permanent || failure.InvalidDestination != test.invalid || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if test.status == 429 && failure.RetryAfter != 2*time.Minute {
				t.Fatal(failure)
			}
		})
	}
}
func TestReceiptMissingFailureAndLookupErrorDiffer(t *testing.T) {
	for _, body := range []string{`{"data":{}}`, `{"data":{"ticket":{"status":"error","details":{"error":"DeviceNotRegistered"}}}}`, `{"data":null}`} {
		sender, _ := NewExpo(ExpoConfig{HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return clientResponse(200, body), nil })}})
		result, err := sender.Receipt(t.Context(), "ticket")
		switch body {
		case `{"data":{}}`:
			if result.Found || err != nil {
				t.Fatal(result, err)
			}
		case `{"data":null}`:
			if err == nil {
				t.Fatal("missing data accepted")
			}
		default:
			if err != nil || !result.Found || result.Error == nil || !result.Error.InvalidDestination {
				t.Fatal(result, err)
			}
		}
	}
}
func TestExpoRejectsInvalidInputAndCancellation(t *testing.T) {
	sender, _ := NewExpo(ExpoConfig{HTTPClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}})
	if _, err := sender.Send(t.Context(), "old-fcm-token", Message{Title: "Test"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sender.Send(ctx, "ExpoPushToken[phone]", Message{Title: "Test"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewExpo(ExpoConfig{AccessToken: "secret\nheader"}); err == nil {
		t.Fatal("unsafe credential")
	}
}
