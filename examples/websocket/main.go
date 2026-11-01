// This local-only example serves an unauthenticated echo socket and an HTTP
// protocol probe. Production socket routes must apply their own authentication.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/websocket"
	"github.com/wisalgroup/labena/pkg/httpserver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	settings := cfg.HTTP.ServerConfig()
	// Keep an unauthenticated demonstration private, while retaining configured TLS.
	_, port, err := net.SplitHostPort(settings.Address)
	if err != nil {
		return err
	}
	settings.Address = net.JoinHostPort("127.0.0.1", port)
	router, err := api.New(api.Options{DisableDocs: true})
	if err != nil {
		return err
	}
	socket, err := websocket.New(websocket.Config{}, func(ctx context.Context, conn *websocket.Conn) error {
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if err := conn.Write(ctx, kind, data); err != nil {
				return err
			}
		}
	})
	if err != nil {
		return err
	}
	router.Mux.Handle("GET /ws", socket)
	router.Mux.HandleFunc("GET /protocol", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, r.Proto) })
	server, err := httpserver.New(settings, router)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return server.Run(ctx)
}
