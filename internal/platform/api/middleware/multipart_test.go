package middleware

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func TestMultipartBoundsAndCleanup(t *testing.T) {
	for _, mode := range []string{"success", "handler panic", "too large", "truncated", "oversize epilogue", "unauthenticated"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "private.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(part, strings.NewReader(strings.Repeat("a", 2<<20)))
			_ = writer.Close()
			raw := body.Bytes()
			if mode == "truncated" {
				raw = raw[:len(raw)-20]
			}
			if mode == "oversize epilogue" {
				raw = append(raw, bytes.Repeat([]byte("a"), 2<<20)...)
			}
			limit := int64(3 << 20)
			if mode == "too large" {
				limit = 1 << 20
			}
			router := http.NewServeMux()
			h := humago.New(router, huma.DefaultConfig("test", "1"))
			if mode == "unauthenticated" {
				h.UseMiddleware(Bearer(nil))
			}
			calls := 0
			huma.Register(h, huma.Operation{Method: "POST", Path: "/", MaxBodyBytes: limit, Middlewares: huma.Middlewares{Multipart}}, func(_ context.Context, in *struct{ RawBody multipart.Form }) (*struct{}, error) {
				calls++
				entries, _ := os.ReadDir(dir)
				if len(entries) == 0 {
					t.Error("large upload did not spool to disk")
				}
				file, err := in.RawBody.File["file"][0].Open()
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				if mode == "handler panic" {
					panic("test panic")
				}
				return nil, nil
			})
			req := httptest.NewRequest("POST", "/", bytes.NewReader(raw))
			req.ContentLength = -1
			req.Header.Set("Content-Type", writer.FormDataContentType())
			out := httptest.NewRecorder()
			func() {
				defer func() {
					if v := recover(); v != nil && mode != "handler panic" {
						t.Errorf("unexpected panic: %v", v)
					}
				}()
				router.ServeHTTP(out, req)
			}()
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatalf("temporary files leaked: %v", entries)
			}
			switch mode {
			case "success":
				if calls != 1 || out.Code != 204 {
					t.Fatal(calls, out.Code, out.Body.String())
				}
			case "handler panic":
				if calls != 1 {
					t.Fatal(calls)
				}
			default:
				want := 400
				if mode == "too large" || mode == "oversize epilogue" {
					want = 413
				}
				if mode == "unauthenticated" {
					want = 401
				}
				if calls != 0 || out.Code != want {
					t.Fatal(calls, out.Code, out.Body.String())
				}
			}
		})
	}
}
