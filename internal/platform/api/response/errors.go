package response

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/pkg/apperror"
	"github.com/wisalgroup/labena/pkg/logging"
)

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
	if errors.As(err, &size) {
		status, code, detail = 413, "request_too_large", "The request body is too large."
	} else if errors.As(err, &app) && app != nil {
		statuses := map[apperror.Code]int{apperror.CodeInvalidInput: 400, apperror.CodeValidation: 422, apperror.CodeUnauthenticated: 401, apperror.CodeForbidden: 403, apperror.CodeNotFound: 404, apperror.CodeConflict: 409, apperror.CodeRateLimited: 429, apperror.CodeUnavailable: 503}
		if s, ok := statuses[app.Code()]; ok {
			status, code, detail = s, string(app.Code()), app.Message()
			fields = app.Fields()
		}
	}
	p := &Problem{ErrorModel: huma.ErrorModel{Type: "urn:go-modular:error:" + code, Title: http.StatusText(status), Status: status, Detail: detail}}
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
	return &failure{Problem: errorDetails(err), cause: err}
}

// WriteError rejects a request from Huma middleware. Return without invoking next.
func WriteError(ctx huma.Context, err error) {
	LogError(ctx.Context(), err)
	writeProblem(ctx.BodyWriter(), ctx.SetHeader, ctx.SetStatus, errorDetails(err))
}

// WriteHTTPError writes a service failure from a standard net/http handler.
// Call before committing headers or body bytes, then return from the handler.
func WriteHTTPError(ctx context.Context, w http.ResponseWriter, err error) {
	LogError(ctx, err)
	writeProblem(w, w.Header().Set, w.WriteHeader, errorDetails(err))
}

func writeProblem(w interface{ Write([]byte) (int, error) }, header func(string, string), status func(int), p *Problem) {
	header("Content-Type", "application/problem+json")
	status(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// Transform integrates safe problem details with Huma without changing its global
// hooks. Huma owns validation; only redact transport diagnostics here: no submitted
// values or parser internals leave the HTTP boundary, including missing fields.
func Transform(_ huma.Context, _ string, value any) (any, error) {
	source, ok := value.(*huma.ErrorModel)
	if !ok {
		return value, nil
	}
	p := *source
	names := map[int]string{400: "invalid_input", 401: "unauthenticated", 403: "forbidden", 404: "not_found", 409: "conflict", 406: "not_acceptable", 408: "request_timeout", 413: "request_too_large", 415: "unsupported_media_type", 422: "validation_failed", 429: "rate_limited", 503: "unavailable"}
	code := names[p.Status]
	if code == "" {
		code = "http_error"
		if p.Status >= 500 {
			code = "internal_error"
		}
	}
	p.Type = "urn:go-modular:error:" + code
	p.Title = http.StatusText(p.Status)
	p.Detail = p.Title
	p.Errors = nil
	if p.Status >= 500 {
		return &Problem{ErrorModel: p}, nil
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
	return &Problem{ErrorModel: p}, nil
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
	logging.FromContext(ctx).ErrorContext(ctx, "HTTP operation failed", "error_category", logging.ErrorCategory(err), "error_code", p.Type)
}
