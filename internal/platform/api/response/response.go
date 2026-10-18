// Package response constructs typed Huma outputs and safe HTTP problem details.
// Huma owns serialization, headers and OpenAPI; files and streams use native outputs.
package response

import (
	"context"
	"maps"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Envelope keeps the result's concrete type available to Huma's schema generator.
// Data is always present, including null for a nil result.
type Envelope[T any] struct {
	Data T              `json:"data"`
	Meta map[string]any `json:"meta,omitempty" doc:"Optional operation-specific metadata"`
}

// JSON is a native Huma output. Construct it with NewJSON or a status shortcut;
// Huma uses Status directly, so a literal with an unset Status is invalid.
type JSON[T any] struct {
	Body   Envelope[T]
	Status int
}

// NewJSON constructs a successful response whose status permits a JSON body.
// Invalid status values are programmer errors. Declare the same success status
// on the Huma operation so its OpenAPI contract matches the runtime response.
func NewJSON[T any](data T, status int) *JSON[T] {
	if status < 200 || status >= 300 || status == http.StatusNoContent || status == http.StatusResetContent {
		panic("response: JSON requires a success status that permits a body")
	}
	return &JSON[T]{Body: Envelope[T]{Data: data}, Status: status}
}

func OK[T any](data T) *JSON[T]       { return NewJSON(data, http.StatusOK) }
func Created[T any](data T) *JSON[T]  { return NewJSON(data, http.StatusCreated) }
func Accepted[T any](data T) *JSON[T] { return NewJSON(data, http.StatusAccepted) }

// WithMeta sets optional JSON object metadata. It copies the map; nested values
// remain caller-owned and must not be mutated while the response is being sent.
func (r *JSON[T]) WithMeta(meta map[string]any) *JSON[T] {
	r.Body.Meta = maps.Clone(meta)
	return r
}

// Empty declares no response body. Huma defaults bodyless outputs to status 204.
type Empty struct{}

func NoContent() *Empty { return &Empty{} }

// Problem retains Huma's RFC 9457 fields and exhaustive errors list, with optional
// operation-specific metadata. There is no surrounding envelope.
type Problem struct {
	huma.ErrorModel
	Meta map[string]any `json:"meta,omitempty" doc:"Optional operation-specific metadata"`
}

// WithRequestID attaches the server-generated correlation ID to a request context.
// The API server calls this once before middleware and handlers run.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

type requestIDKey struct{}

// RequestID returns the server-generated correlation ID for this request.
// It returns an empty string when no ID has been attached to the context.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
