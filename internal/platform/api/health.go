package api

import (
	"context"
	"net/http"
)

// RegisterHealthRoutes mounts unversioned health endpoints. App owns the signals:
// ready closes after startup; stopping closes before drain. Nil signals stay pending.
// Optional check must honor its context and impose a deadline. Only one check runs
// at a time; concurrent probes return 503 instead of adding dependency load.
// Dependency failures are deliberately not exposed in the response.
func RegisterHealthRoutes(router *http.ServeMux, ready, stopping <-chan struct{}, check func(context.Context) error) {
	admission := make(chan struct{}, 1)
	router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stopping:
			http.Error(w, "stopping", http.StatusServiceUnavailable)
			return
		default:
		}
		select {
		case <-ready:
		default:
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		if check != nil {
			select {
			case admission <- struct{}{}:
				defer func() { <-admission }()
			default:
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			if err := check(r.Context()); err != nil {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		// Shutdown may have started while dependencies were being checked.
		select {
		case <-stopping:
			http.Error(w, "stopping", http.StatusServiceUnavailable)
			return
		default:
		}
		w.WriteHeader(http.StatusOK)
	})
}
