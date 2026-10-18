// Package apperror describes safe application failures without transport dependencies.
// Wrap errors with fmt.Errorf("operation: %w", err); use errors.Is/As to inspect them.
package apperror

// Code classifies a failure independently of HTTP or gRPC status codes.
// Transport adapters must treat unknown codes as internal failures.
type Code string

const (
	CodeInternal        Code = "internal_error"
	CodeInvalidInput    Code = "invalid_input"
	CodeValidation      Code = "validation_failed"
	CodeUnauthenticated Code = "unauthenticated"
	CodeForbidden       Code = "forbidden"
	CodeNotFound        Code = "not_found"
	CodeConflict        Code = "conflict"
	CodeUnavailable     Code = "unavailable"
	CodeRateLimited     Code = "rate_limited"
)

// Error carries an explicitly public code and message. Fields are private so
// package-level domain errors can be shared safely between concurrent callers.
// Error identity is pointer identity: errors.Is matches the original sentinel.
type Error struct {
	code    Code
	message string
	fields  map[string][]string
}

// New creates an application error. Code and message must never contain secrets,
// raw database errors, or untrusted input. Internal errors are hidden by transports.
func New(code Code, message string) *Error {
	return &Error{code: code, message: message}
}

// Internal creates an error with CodeInternal.
func Internal(message string) *Error { return New(CodeInternal, message) }

// InvalidInput creates an error with CodeInvalidInput.
func InvalidInput(message string) *Error { return New(CodeInvalidInput, message) }

// Validation creates an error with CodeValidation.
func Validation(message string) *Error { return New(CodeValidation, message) }

// Unauthenticated creates an error with CodeUnauthenticated.
func Unauthenticated(message string) *Error { return New(CodeUnauthenticated, message) }

// Forbidden creates an error with CodeForbidden.
func Forbidden(message string) *Error { return New(CodeForbidden, message) }

// NotFound creates an error with CodeNotFound.
func NotFound(message string) *Error { return New(CodeNotFound, message) }

// Conflict creates an error with CodeConflict.
func Conflict(message string) *Error { return New(CodeConflict, message) }

// Unavailable creates an error with CodeUnavailable.
func Unavailable(message string) *Error { return New(CodeUnavailable, message) }

// RateLimited creates an error with CodeRateLimited.
func RateLimited(message string) *Error { return New(CodeRateLimited, message) }

func (e *Error) Error() string   { return e.message }
func (e *Error) Code() Code      { return e.code }
func (e *Error) Message() string { return e.message }

// InvalidFields copies field errors. Keys are public input names, using dot paths
// for nested fields. Values are safe messages, never submitted values.
func InvalidFields(fields map[string][]string) *Error {
	return &Error{code: CodeValidation, message: "The given data was invalid.", fields: clone(fields)}
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
