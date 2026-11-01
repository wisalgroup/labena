package middleware

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/pkg/apperror"
)

type baseContext = huma.Context

type multipartContext struct {
	baseContext
	form *multipart.Form
}

func (c multipartContext) GetMultipartForm() (*multipart.Form, error) { return c.form, nil }

// Multipart bounds the complete request, spools files above 1 MiB to disk, and
// removes temporary files after Huma and the handler return, including on failure.
// Use RawBody multipart.Form and set the operation's MaxBodyBytes explicitly.
// Register after authentication; this middleware is the sole multipart reader.
func Multipart(ctx huma.Context, next func(huma.Context)) {
	limit := ctx.Operation().MaxBodyBytes
	if limit <= 0 {
		panic("api: multipart requires a positive MaxBodyBytes")
	}
	timeout := ctx.Operation().BodyReadTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if timeout > 0 {
		_ = ctx.SetReadDeadline(time.Now().Add(timeout))
		defer ctx.SetReadDeadline(time.Time{})
	}
	reader := http.MaxBytesReader(nil, io.NopCloser(ctx.BodyReader()), limit)
	req := &http.Request{Method: "POST", Header: http.Header{"Content-Type": {ctx.Header("Content-Type")}}, Body: reader}
	err := req.ParseMultipartForm(min(limit, 1<<20))
	if req.MultipartForm != nil {
		defer func() { response.LogError(ctx.Context(), req.MultipartForm.RemoveAll()) }()
	}
	// Include any epilogue in the total byte budget, even after a closing boundary.
	if err == nil {
		_, err = io.Copy(io.Discard, reader)
	}
	if err != nil {
		var size *http.MaxBytesError
		if !errors.As(err, &size) {
			err = apperror.InvalidInput("The multipart request is invalid.")
		}
		response.WriteError(ctx, err)
		return
	}
	next(multipartContext{baseContext: ctx, form: req.MultipartForm})
}
