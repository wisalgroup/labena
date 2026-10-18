package push

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ExpoConfig borrows HTTPClient; nil uses a bounded default. AccessToken is the
// optional Expo enhanced push security token, never an application access token.
type ExpoConfig struct {
	AccessToken string
	HTTPClient  *http.Client `json:"-"`
}

func (c ExpoConfig) Validate() error {
	if strings.ContainsFunc(c.AccessToken, unicode.IsSpace) || strings.ContainsFunc(c.AccessToken, unicode.IsControl) {
		return ErrInvalid
	}
	return nil
}

// Expo is safe to share. It starts no workers, does not retry automatically and
// bounds concurrency to six requests per instance. Reuse one instance per process.
type Expo struct {
	client http.Client
	token  string
	slots  chan struct{}
}

func NewExpo(c ExpoConfig) (*Expo, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 30 * time.Second}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	// Never forward the access token through a redirect, including custom clients.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Expo{client: client, token: c.AccessToken, slots: make(chan struct{}, 6)}, nil
}

var expoToken = regexp.MustCompile(`^(ExpoPushToken|ExponentPushToken)\[[A-Za-z0-9_-]+\]$`)

// ValidToken checks documented Expo token syntax, not ownership or provider validity.
func ValidToken(token string) bool { return len(token) <= 4096 && expoToken.MatchString(token) }
func validTicket(id string) bool {
	return id != "" && len(id) <= 200 && !strings.ContainsFunc(id, unicode.IsSpace) && !strings.ContainsFunc(id, unicode.IsControl)
}

type expoResult struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

func (e *Expo) Send(ctx context.Context, token string, m Message) (Ticket, error) {
	if !ValidToken(token) {
		return Ticket{}, ErrInvalid
	}
	if err := m.Validate(); err != nil {
		return Ticket{}, err
	}
	input := map[string]any{"to": token, "title": m.Title, "body": m.Body, "data": m.Data, "sound": "default"}
	if m.ExpiresAt != nil {
		if !m.ExpiresAt.After(time.Now()) {
			return Ticket{}, &Error{Code: "expired", Permanent: true}
		}
		input["expiration"] = m.ExpiresAt.Unix()
	}
	var result expoResult
	if err := e.call(ctx, "send", input, &result); err != nil {
		return Ticket{}, err
	}
	if result.Status == "error" {
		return Ticket{}, expoError(result.Details.Error)
	}
	if result.Status != "ok" || !validTicket(result.ID) {
		return Ticket{}, &Error{Code: "invalid_response"}
	}
	return Ticket{ID: result.ID}, nil
}
func (e *Expo) Receipt(ctx context.Context, id string) (Receipt, error) {
	if !validTicket(id) {
		return Receipt{}, ErrInvalid
	}
	var results map[string]expoResult
	if err := e.call(ctx, "getReceipts", map[string]any{"ids": []string{id}}, &results); err != nil {
		return Receipt{}, err
	}
	result, ok := results[id]
	if !ok {
		return Receipt{}, nil
	}
	switch result.Status {
	case "ok":
		return Receipt{Found: true}, nil
	case "error":
		return Receipt{Found: true, Error: expoError(result.Details.Error)}, nil
	default:
		return Receipt{}, &Error{Code: "invalid_response"}
	}
}
func expoError(code string) *Error {
	switch code {
	case "DeviceNotRegistered":
		return &Error{Code: "unregistered", Permanent: true, InvalidDestination: true}
	case "MessageRateExceeded", "TOO_MANY_REQUESTS":
		return &Error{Code: "rate_limited", RetryAfter: time.Minute}
	case "InvalidCredentials", "MismatchSenderId", "UNAUTHORIZED":
		return &Error{Code: "credentials", Permanent: true}
	case "MessageTooBig", "PUSH_TOO_MANY_NOTIFICATIONS", "PUSH_TOO_MANY_RECEIPTS", "PUSH_TOO_MANY_EXPERIENCE_IDS":
		return &Error{Code: "invalid_argument", Permanent: true}
	default:
		return &Error{Code: "provider_rejected", Permanent: true}
	}
}
func (e *Expo) call(ctx context.Context, method string, input, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	data, err := json.Marshal(input)
	if err != nil {
		return ErrInvalid
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://exp.host/--/api/v2/push/"+method, bytes.NewReader(data))
	if err != nil {
		return ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	response, err := e.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Code: "unavailable"}
	}
	defer response.Body.Close()
	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == 429:
			return &Error{Code: "rate_limited", RetryAfter: max(time.Minute, retryAfter)}
		case response.StatusCode >= 500:
			return &Error{Code: "unavailable", RetryAfter: retryAfter}
		case response.StatusCode == 401 || response.StatusCode == 403:
			return &Error{Code: "credentials", Permanent: true}
		default:
			return &Error{Code: "invalid_request", Permanent: true}
		}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return &Error{Code: "invalid_response"}
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return &Error{Code: "invalid_response"}
	}
	if len(envelope.Errors) > 0 {
		failure := expoError(envelope.Errors[0].Code)
		failure.RetryAfter = max(failure.RetryAfter, retryAfter)
		return failure
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, output) != nil {
		return &Error{Code: "invalid_response"}
	}
	return nil
}
func parseRetryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= int64((1<<63-1)/time.Second) {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(when))
	}
	return 0
}
