package httpclient

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrResponseTooLarge means Do retained only MaxResponseBytes of the body.
var ErrResponseTooLarge = errors.New("httpclient: response exceeds MaxResponseBytes")

// Response retains the response bytes. Decoding does not consume Body. Trailer
// is complete only after a successful body read. Use DoRequest for streaming.
type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Trailer    http.Header
	Body       []byte
	bodyErr    error
}

// Text returns the retained body as a string, without consuming it.
func (r *Response) Text() string { return string(r.Body) }

// BodyError returns the read, size-limit or close error also returned by Do.
// Body and Text remain available for diagnostics; decoders reject incomplete data.
func (r *Response) BodyError() error { return r.bodyErr }

// Successful reports whether the HTTP status is 2xx. It does not imply the body
// was read completely or the provider accepted the operation; inspect Do's error
// and any provider-specific error envelope separately.
func (r *Response) Successful() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

// ContentType returns the normalized media type without parameters, or an empty
// string if Content-Type is absent or malformed. Decoding is selected explicitly.
func (r *Response) ContentType() string {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return ""
	}
	return mediaType
}

// RetryAfter reads a delay in seconds or an HTTP date. The boolean distinguishes
// a valid zero delay from an absent, malformed or unrepresentable value. Past
// dates yield zero. This only reads the hint; it never schedules or retries work.
func (r *Response) RetryAfter() (time.Duration, bool) {
	value := strings.TrimSpace(r.Header.Get("Retry-After"))
	const maxDelay = time.Duration(1<<63 - 1)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(maxDelay/time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	now := time.Now()
	if !date.After(now) {
		return 0, true
	}
	if date.After(now.Add(maxDelay)) {
		return 0, false
	}
	return date.Sub(now), true
}

// DecodeJSON decodes one complete JSON value without consuming Body. Numbers
// decoded into any/map[string]any use json.Number to avoid precision loss; typed
// numeric fields keep their usual behavior. Empty bodies return a DecodeError
// wrapping io.EOF. Content-Type does not restrict this explicit operation.
func (r *Response) DecodeJSON(target any) error {
	if r.bodyErr != nil {
		return &DecodeError{Format: "json", Err: r.bodyErr}
	}
	decoder := json.NewDecoder(bytes.NewReader(r.Body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return &DecodeError{Format: "json", Err: err}
	}
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		err = errors.New("multiple JSON values")
	}
	return &DecodeError{Format: "json", Err: err}
}

// DecodeXML decodes one XML document without consuming Body. A second root or
// trailing non-whitespace data is rejected; comments and processing instructions
// in the epilog are permitted. Empty bodies return a DecodeError wrapping io.EOF.
func (r *Response) DecodeXML(target any) error {
	if r.bodyErr != nil {
		return &DecodeError{Format: "xml", Err: r.bodyErr}
	}
	decoder := xml.NewDecoder(bytes.NewReader(r.Body))
	if err := decoder.Decode(target); err != nil {
		return &DecodeError{Format: "xml", Err: err}
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return &DecodeError{Format: "xml", Err: err}
		}
		switch token := token.(type) {
		case xml.CharData:
			if strings.Trim(string(token), " \t\r\n") == "" {
				continue
			}
		case xml.Comment:
			continue
		case xml.ProcInst:
			if !strings.EqualFold(token.Target, "xml") {
				continue
			}
		}
		return &DecodeError{Format: "xml", Err: errors.New("unexpected data after XML document")}
	}
}

// DecodeError identifies the requested format and retains the cause for
// errors.Is/As without copying potentially private provider content into Error.
// As with standard decoders, ignore the target value when decoding fails.
type DecodeError struct {
	Format string
	Err    error
}

func (e *DecodeError) Error() string { return "httpclient: cannot decode " + e.Format + " response" }
func (e *DecodeError) Unwrap() error { return e.Err }

// CheckStatus accepts any 2xx response. Provider-level errors in a successful
// HTTP response belong to the integration, for example GraphQL errors.
func (r *Response) CheckStatus() error {
	if r.Successful() {
		return nil
	}
	return &StatusError{Response: r}
}

// StatusError preserves the response for inspection without including its body,
// URL or headers in Error. Inspect it with errors.As.
type StatusError struct{ Response *Response }

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpclient: HTTP %d %s", e.Response.StatusCode, http.StatusText(e.Response.StatusCode))
}

func readResponse(result *http.Response, limit int64) (*Response, error) {
	body, err := io.ReadAll(io.LimitReader(result.Body, limit+1))
	err = errors.Join(err, result.Body.Close())
	if int64(len(body)) > limit {
		body = body[:limit]
		err = errors.Join(err, ErrResponseTooLarge)
	}
	return &Response{StatusCode: result.StatusCode, Status: result.Status,
		Header: result.Header.Clone(), Trailer: result.Trailer.Clone(), Body: body, bodyErr: err}, err
}
