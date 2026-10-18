package api

import (
	"encoding/json"
	"net/http"

	"github.com/wisalgroup/labena/internal/platform/api/response"
)

// Render the registered schema on demand. Registration ends before serving, so
// concurrent requests only read it and there is no mutable lazy cache to race.
func (s *Server) registerDocs() {
	s.Mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		data, err := json.Marshal(s.OpenAPI())
		if err != nil {
			response.WriteHTTPError(r.Context(), w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
	s.Mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; connect-src 'self'; frame-ancestors 'none'; script-src 'unsafe-eval' https://unpkg.com/@scalar/api-reference@1.44.20/dist/browser/standalone.js; style-src 'unsafe-inline'")
		_, _ = w.Write([]byte(docsHTML))
	})
}

const docsHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width,initial-scale=1"><title>API Reference</title></head>
<body><script id="api-reference" data-url="./openapi.json"></script>
<script src="https://unpkg.com/@scalar/api-reference@1.44.20/dist/browser/standalone.js" crossorigin integrity="sha384-tMz7GAo6dMy55x9tLFtH+sHtogji6Scmb+feBR31TAHmvSPRUTboK9H3M5NFaP4R"></script></body></html>`
