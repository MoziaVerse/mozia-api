package metrics

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

// Server serves Prometheus /metrics on an isolated internal listener so metrics
// are never accidentally exposed on the public API port.
type Server struct {
	httpServer *http.Server
	addr       string
}

// StartServerFromEnv starts the internal Prometheus HTTP server when
// METRICS_ADDR is set or METRICS_ENABLED=true. Returns (nil, nil) if disabled.
func StartServerFromEnv(r *Recorder, onError func(error)) (*Server, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("METRICS_ENABLED")), "false") {
		return nil, nil
	}
	addr := strings.TrimSpace(os.Getenv("METRICS_ADDR"))
	if addr == "" && strings.EqualFold(strings.TrimSpace(os.Getenv("METRICS_ENABLED")), "true") {
		addr = ":9091"
	}
	if addr == "" {
		return nil, nil
	}
	token := strings.TrimSpace(os.Getenv("METRICS_TOKEN"))
	return StartServer(r, addr, token, onError)
}

// StartServer starts a dedicated HTTP server serving /metrics at addr.
func StartServer(r *Recorder, addr, bearerToken string, onError func(error)) (*Server, error) {
	if r == nil {
		r = Default()
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", wrapBearerAuth(r.Handler(), bearerToken))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if onError != nil {
				onError(err)
			}
		}
	}()

	return &Server{httpServer: srv, addr: addr}, nil
}

// Addr returns the configured listen address of the metrics server.
func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	return s.addr
}

// Shutdown gracefully stops the internal metrics HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func wrapBearerAuth(next http.Handler, token string) http.Handler {
	if token == "" {
		return next
	}
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		provided := []byte(req.Header.Get("Authorization"))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, req)
	})
}
