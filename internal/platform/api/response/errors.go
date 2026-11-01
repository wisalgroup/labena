package response

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/logging"
)

// ErrorTypePrefix identifies the default Labena problem types.
const ErrorTypePrefix = "urn:labena:error:"

var appErrorKindMap = map[apperror.Kind]int{
	apperror.KindInvalidInput:    400,
	apperror.KindUnauthenticated: 401,
	apperror.KindForbidden:       403,
	apperror.KindNotFound:        404,
	apperror.KindConflict:        409,
	apperror.KindValidation:      422,
	apperror.KindRateLimited:     429,
	apperror.KindUnavailable:     503,
}

type failure struct {
	*Problem
	cause error
}

func (f *failure) Unwrap() error { return f.cause }

func errorDetails(err error) *Problem {
	status, code, detail := 500, "internal_error", "An unexpected error occurred."
	var app *apperror.Error
	var size *http.MaxBytesError
	var fields map[string][]string
	var problemType, title string
	if errors.As(err, &size) {
		status, code, detail = 413, "request_too_large", "The request body is too large."
	} else if errors.As(err, &app) && app != nil {
		if s, ok := appErrorKindMap[app.Kind()]; ok {
			status, code, detail = s, string(app.Code()), app.Detail()
			fields = app.Fields()
			problemType = app.Type()
			if problemType == "" {
				problemType = ErrorTypePrefix + string(app.Kind())
			}
			title = app.Title()
		}
	}
	if problemType == "" {
		problemType = ErrorTypePrefix + code
	}
	if title == "" {
		title = http.StatusText(status)
	}
	p := &Problem{ErrorModel: huma.ErrorModel{Type: problemType, Title: title, Status: status, Detail: detail}, Code: code}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, message := range fields[k] {
			p.Errors = append(p.Errors, &huma.ErrorDetail{Location: validationLocation(k), Message: message})
		}
	}
	return p
}

// Bare application fields refer to the request body. Explicit transport paths
// remain available for validation performed at the HTTP boundary.
func validationLocation(field string) string {
	if field == "" {
		return "body"
	}
	for _, source := range []string{"body", "path", "query", "header", "cookie"} {
		if field == source || strings.HasPrefix(field, source+".") || strings.HasPrefix(field, source+"[") {
			return field
		}
	}
	return "body." + field
}

// Error translates a service failure into safe Huma problem details. Call it when
// returning a service error from an HTTP handler. Nil stays nil; native Huma
// status errors and headers are preserved. No global Huma hooks are changed.
func Error(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var native huma.StatusError
	if errors.As(err, &native) {
		if native.GetStatus() >= 500 {
			LogError(ctx, err)
		}
		return err
	}
	LogError(ctx, err)
	return &failure{Problem: withInstance(ctx, errorDetails(err)), cause: err}
}

// WriteError rejects a request from Huma middleware. Return without invoking next.
func WriteError(ctx huma.Context, err error) {
	LogError(ctx.Context(), err)
	writeProblem(ctx.BodyWriter(), ctx.SetHeader, ctx.SetStatus, withInstance(ctx.Context(), errorDetails(err)))
}

// WriteHTTPError writes a service failure from a standard net/http handler.
// Call before committing headers or body bytes, then return from the handler.
func WriteHTTPError(ctx context.Context, w http.ResponseWriter, err error) {
	LogError(ctx, err)
	writeProblem(w, w.Header().Set, w.WriteHeader, withInstance(ctx, errorDetails(err)))
}

func withInstance(ctx context.Context, p *Problem) *Problem {
	p.Instance = problemInstance(ctx)
	return p
}

func problemInstance(ctx context.Context) string {
	id, err := uuid.Parse(RequestID(ctx))
	if err != nil || id == uuid.Nil {
		id = uuid.New()
	}
	return id.URN()
}

func writeProblem(w interface{ Write([]byte) (int, error) }, header func(string, string), status func(int), p *Problem) {
	header("Content-Type", "application/problem+json")
	status(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// Transform integrates safe problem details with Huma without changing its global
// hooks. Huma owns validation; only redact transport diagnostics here: no submitted
// values or parser internals leave the HTTP boundary, including missing fields.
func Transform(ctx huma.Context, _ string, value any) (any, error) {
	source, ok := value.(*huma.ErrorModel)
	if !ok {
		return value, nil
	}
	p := *source
	code := codeForStatus(p.Status)
	p.Type = ErrorTypePrefix + code
	p.Title = http.StatusText(p.Status)
	p.Detail = ""
	p.Instance = problemInstance(ctx.Context())
	p.Errors = nil
	if p.Status >= 500 {
		p.Detail = "An unexpected error occurred."
		return &Problem{ErrorModel: p, Code: code}, nil
	}
	for _, e := range source.Errors {
		if e == nil {
			continue
		}
		message := "This value is invalid."
		if strings.HasPrefix(e.Message, "expected required property ") && strings.HasSuffix(e.Message, " to be present") {
			message = e.Message
		}
		p.Errors = append(p.Errors, &huma.ErrorDetail{Location: e.Location, Message: message})
	}
	return &Problem{ErrorModel: p, Code: code}, nil
}

// codeForStatus classifies native HTTP failures without inferring a business cause.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return string(apperror.KindInvalidInput)
	case http.StatusUnauthorized:
		return string(apperror.KindUnauthenticated)
	case http.StatusForbidden:
		return string(apperror.KindForbidden)
	case http.StatusNotFound:
		return string(apperror.KindNotFound)
	case http.StatusConflict:
		return string(apperror.KindConflict)
	case http.StatusUnprocessableEntity:
		return string(apperror.KindValidation)
	case http.StatusTooManyRequests:
		return string(apperror.KindRateLimited)
	case http.StatusServiceUnavailable:
		return string(apperror.KindUnavailable)
	case http.StatusNotAcceptable:
		return "not_acceptable"
	case http.StatusRequestTimeout:
		return "request_timeout"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	default:
		if status >= http.StatusInternalServerError {
			return string(apperror.KindInternal)
		}
		return "http_error"
	}
}

// LogError records a safe operational failure, including errors after a stream
// starts. It never writes a response; a broken stream must abort its transport.
func LogError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	p := errorDetails(err)
	if p.Status < 500 {
		return
	}
	trace.SpanFromContext(ctx).SetStatus(codes.Error, "HTTP operation failed")
	logging.FromContext(ctx).ErrorContext(ctx, "HTTP operation failed", "error_category", logging.ErrorCategory(err), "error_code", p.Code)
}
