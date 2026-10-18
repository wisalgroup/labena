// Package api assembles Huma on Go's HTTP server with shared request policies.
package api

import (
	"errors"
	"net/http"
	"net/netip"
	"reflect"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/wisalgroup/labena/internal/platform/api/response"
)

// Options configures a server without starting a listener. Zero values are usable.
// Middleware contains standard net/http middleware and runs inside observability.
type Options struct {
	Title, Version string
	// APIVersion identifies the business API in X-API-Version (default v1).
	// Version above identifies the application release in the OpenAPI document.
	APIVersion string
	// DisableDocs hides both /docs and /openapi.json. They are enabled by default.
	DisableDocs bool
	Tracer      trace.Tracer
	Meter       metric.Meter
	// TrustTraceContext accepts remote trace parents from a trusted ingress.
	TrustTraceContext bool
	// TrustedProxies may supply X-Forwarded-For. Empty trusts no forwarding headers.
	// The deployment must secure these proxy connections and sanitize the chain.
	TrustedProxies []netip.Prefix
	Middleware     []func(http.Handler) http.Handler
}

// Server is a Huma API and an http.Handler. Mux accepts infrastructure handlers;
// modules register typed operations on the embedded API or native Huma groups.
// Finish all registration before serving. The application owns listener lifecycle.
type Server struct {
	huma.API
	Mux     *http.ServeMux
	handler http.Handler
}

// New creates an API with safe defaults and live documentation. It starts no I/O.
func New(options Options) (*Server, error) {
	if options.Title == "" {
		options.Title = "Application API"
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	options.TrustedProxies = slices.Clone(options.TrustedProxies)
	for _, prefix := range options.TrustedProxies {
		if !validProxyPrefix(prefix) {
			return nil, errors.New("api: invalid trusted proxy CIDR")
		}
	}
	if options.APIVersion == "" {
		options.APIVersion = "v1"
	}
	config := huma.DefaultConfig(options.Title, options.Version)
	// Keep response bodies identical to their Go types; schemas remain in OpenAPI.
	config.CreateHooks = nil
	config.SchemasPath = ""
	config.DocsPath, config.OpenAPIPath = "", ""
	config.Formats = map[string]huma.Format{"application/json": {Marshal: huma.DefaultJSONFormat.Marshal, Unmarshal: decodeJSON}, "json": {Marshal: huma.DefaultJSONFormat.Marshal, Unmarshal: decodeJSON}}
	config.DefaultFormat = "application/json"
	config.Transformers = []huma.Transformer{response.Transform}
	// Document problem details and optional meta without changing Huma's global hooks.
	config.Components.Schemas.RegisterTypeAlias(reflect.TypeFor[huma.ErrorModel](), reflect.TypeFor[response.Problem]())
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{"Bearer": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"}}
	mux := http.NewServeMux()
	server := &Server{API: humago.New(mux, config), Mux: mux}
	server.UseMiddleware(requestEncoding(server))
	if !options.DisableDocs {
		server.registerDocs()
	}
	var handler http.Handler = canonicalPath(mux)
	for i := len(options.Middleware) - 1; i >= 0; i-- {
		handler = options.Middleware[i](handler)
	}
	var err error
	server.handler, err = observe(mux, handler, options)
	if err != nil {
		return nil, err
	}
	return server, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
