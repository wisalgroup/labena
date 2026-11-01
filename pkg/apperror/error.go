// Package apperror describes safe application failures without transport dependencies.
// Wrap errors with fmt.Errorf("operation: %w", err); use errors.Is/As to inspect them.
package apperror

import (
	"net/url"
	"strings"
	"unicode"
)

// Kind classifies a failure independently of HTTP or gRPC status codes.
// Transport adapters must treat unknown kinds as internal failures.
type Kind string

const (
	KindInternal        Kind = "internal_error"
	KindInvalidInput    Kind = "invalid_input"
	KindValidation      Kind = "validation_failed"
	KindUnauthenticated Kind = "unauthenticated"
	KindForbidden       Kind = "forbidden"
	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindUnavailable     Kind = "unavailable"
	KindRateLimited     Kind = "rate_limited"
)

// Code is a public machine-readable identifier, independent of transport status.
type Code string

// Definition describes a complete, reusable domain error. All fields are required.
type Definition struct {
	Kind  Kind
	Code  Code
	Type  string
	Title string
}

// Define creates a reusable error with no occurrence detail. It panics for an
// unknown kind, incomplete definition or invalid absolute type URI. Definitions
// are static developer configuration, never request input.
func Define(d Definition) *Error {
	switch d.Kind {
	case KindInternal, KindInvalidInput, KindValidation, KindUnauthenticated,
		KindForbidden, KindNotFound, KindConflict, KindUnavailable, KindRateLimited:
	default:
		panic("apperror: unknown definition kind")
	}
	if strings.TrimSpace(string(d.Code)) == "" || strings.TrimSpace(d.Type) == "" || strings.TrimSpace(d.Title) == "" {
		panic("apperror: definition requires code, type and title")
	}
	return New(d.Kind, "").WithCode(d.Code).WithType(d.Type).WithTitle(d.Title)
}

// Error carries a classification and safe public metadata. Fields are private so
// package-level domain errors can be shared safely between concurrent callers.
// With methods return copies and preserve sentinel identity through errors.Is.
// Errors are safe for concurrent reads; nil receivers are not supported.
// A zero Error has an unknown kind and must be treated as internal by transports.
type Error struct {
	kind        Kind
	code        Code
	title       string
	detail      string
	fields      map[string][]string
	problemType string
	cause       *Error
}

// New creates an application error. Kind and detail must never contain secrets,
// raw database errors, or untrusted input. Internal errors are hidden by transports.
func New(kind Kind, detail string) *Error {
	return &Error{kind: kind, detail: detail}
}

// Internal creates an error with KindInternal.
func Internal(detail string) *Error { return New(KindInternal, detail) }

// InvalidInput creates an error with KindInvalidInput.
func InvalidInput(detail string) *Error { return New(KindInvalidInput, detail) }

// Validation creates an error with KindValidation.
func Validation(detail string) *Error { return New(KindValidation, detail) }

// Unauthenticated creates an error with KindUnauthenticated.
func Unauthenticated(detail string) *Error { return New(KindUnauthenticated, detail) }

// Forbidden creates an error with KindForbidden.
func Forbidden(detail string) *Error { return New(KindForbidden, detail) }

// NotFound creates an error with KindNotFound.
func NotFound(detail string) *Error { return New(KindNotFound, detail) }

// Conflict creates an error with KindConflict.
func Conflict(detail string) *Error { return New(KindConflict, detail) }

// Unavailable creates an error with KindUnavailable.
func Unavailable(detail string) *Error { return New(KindUnavailable, detail) }

// RateLimited creates an error with KindRateLimited.
func RateLimited(detail string) *Error { return New(KindRateLimited, detail) }

// Error returns the detail, title, or public code when no prose was supplied.
func (e *Error) Error() string {
	if e.detail != "" {
		return e.detail
	}
	if e.title != "" {
		return e.title
	}
	return string(e.Code())
}

// Kind returns the transport classification.
func (e *Error) Kind() Kind { return e.kind }

// Code returns the public identifier, defaulting to the kind's identifier.
func (e *Error) Code() Code {
	if e.code != "" {
		return e.code
	}
	return Code(e.kind)
}

// Message returns the detail for compatibility with existing callers.
func (e *Error) Message() string { return e.detail }

// Detail returns the optional safe occurrence description.
func (e *Error) Detail() string { return e.detail }

// Title returns the optional public summary.
func (e *Error) Title() string { return e.title }

// WithCode returns an immutable copy with a public identifier. Empty resets the
// default. It does not change classification, type or title.
func (e *Error) WithCode(code Code) *Error {
	c := e.copy()
	c.code = code
	return c
}

// WithTitle returns an immutable copy with a public summary. Empty resets the default.
func (e *Error) WithTitle(title string) *Error {
	c := e.copy()
	c.title = title
	return c
}

// WithDetail returns an immutable copy with a safe occurrence description.
// Empty omits HTTP detail. Never include secrets or raw infrastructure errors.
func (e *Error) WithDetail(detail string) *Error {
	c := e.copy()
	c.detail = detail
	return c
}

// WithFields returns a copy owning the supplied validation messages.
// It panics for non-validation kinds. Nil clears the fields.
func (e *Error) WithFields(fields map[string][]string) *Error {
	if e.kind != KindValidation {
		panic("apperror: fields require validation kind")
	}
	c := e.copy()
	c.fields = clone(fields)
	return c
}
func (e *Error) copy() *Error {
	c := *e
	c.cause = e
	return &c
}

// Type returns the optional absolute problem-type URI, or empty for the
// transport's default. It refines the failure without changing its Kind.
func (e *Error) Type() string { return e.problemType }

// WithType returns a copy with a public problem-type URI, preserving errors.Is
// identity and validation fields. Empty selects the transport's default type.
// Use a stable developer-defined URN or absolute documentation URI, never input
// or secrets. Invalid nonempty URIs panic as a static programmer error.
func (e *Error) WithType(typeURI string) *Error {
	if typeURI != "" {
		parsed, err := url.ParseRequestURI(typeURI)
		if err != nil || !parsed.IsAbs() || (parsed.Host == "" && parsed.Path == "" && parsed.Opaque == "") ||
			strings.ContainsFunc(typeURI, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			panic("apperror: problem type must be an absolute URI")
		}
	}
	copied := *e
	copied.problemType = typeURI
	copied.cause = e
	return &copied
}

// Unwrap returns the original error when decorated with a With method.
func (e *Error) Unwrap() error {
	if e.cause == nil {
		return nil
	}
	return e.cause
}

// InvalidFields copies field errors. Keys are public input names, using dot paths
// for nested fields. Values are safe messages, never submitted values.
func InvalidFields(fields map[string][]string) *Error {
	return &Error{kind: KindValidation, detail: "The given data was invalid.", fields: clone(fields)}
}

// Fields returns an independent copy; callers cannot mutate shared error state.
func (e *Error) Fields() map[string][]string { return clone(e.fields) }
func clone(fields map[string][]string) map[string][]string {
	if fields == nil {
		return nil
	}
	out := make(map[string][]string, len(fields))
	for name, messages := range fields {
		out[name] = append([]string(nil), messages...)
	}
	return out
}
