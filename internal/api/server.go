package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sml/sml-signx/internal/config"
	"github.com/sml/sml-signx/internal/store"
	"github.com/sml/sml-signx/internal/version"
)

const maxBodyBytes = 128 << 20 // 128 MiB (base64 inflates payloads by ~33%)

// Server exposes the sml-signx REST API.
type Server struct {
	cfg       *config.Config
	store     *store.Store
	logger    *slog.Logger
	startedAt time.Time
	handler   http.Handler
}

// New builds a Server and wires its routes and middleware.
func New(cfg *config.Config, st *store.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		cfg:       cfg,
		store:     st,
		logger:    logger,
		startedAt: time.Now(),
	}
	s.handler = s.recoverer(s.logRequests(s.routes()))
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /{$}", s.handleRoot)

	mux.Handle("GET /op/status", s.authorize(s.withBodyLimit(http.HandlerFunc(s.handleStatus))))
	mux.Handle("GET /op/certificates", s.authorize(s.withBodyLimit(http.HandlerFunc(s.handleListCertificates))))
	mux.Handle("POST /op/register-pfx", s.authorize(s.withBodyLimit(http.HandlerFunc(s.handleRegisterPFX))))
	mux.Handle("POST /op/unregister-pfx", s.authorize(s.withBodyLimit(http.HandlerFunc(s.handleUnregisterPFX))))
	mux.Handle("POST /op/sign", s.authorize(s.withBodyLimit(http.HandlerFunc(s.handleSign))))

	return mux
}

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-API-KEY")
		if got == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				got = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized: invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "sml-signx",
		"version": version.Version,
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, ServiceInfo{
		Service: "sml-signx",
		Version: version.Version,
		Status:  "running",
		Endpoints: []EndpointInfo{
			{Method: "GET", Path: "/healthz", Description: "Liveness probe"},
			{Method: "GET", Path: "/op/status", Description: "Service, certificate and configuration status"},
			{Method: "GET", Path: "/op/certificates", Description: "List registered PFX certificates"},
			{Method: "POST", Path: "/op/register-pfx", Description: "Register a PFX certificate by file path"},
			{Method: "POST", Path: "/op/unregister-pfx", Description: "Remove a registered PFX certificate"},
			{Method: "POST", Path: "/op/sign", Description: "Sign a PDF (Base64 in, Base64 out)"},
		},
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	hostname, _ := os.Hostname()
	certs := s.store.List()
	writeJSON(w, http.StatusOK, StatusResponse{
		Service:              "sml-signx",
		Version:              version.Version,
		Hostname:             hostname,
		Platform:             runtime.GOOS + "/" + runtime.GOARCH,
		Address:              s.cfg.Addr(),
		UptimeSeconds:        int64(time.Since(s.startedAt).Seconds()),
		APIKeyRequired:       s.cfg.APIKey != "",
		RequireRegisteredPFX: s.cfg.RequireRegisteredPFX,
		TSAEnabled:           s.cfg.TSAURL != "",
		CertificateCount:     len(certs),
		Certificates:         certs,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return err
		}
		return err
	}
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}
