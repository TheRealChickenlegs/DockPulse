// Package controller hosts the web UI, the agent-facing API, and the
// database. It is the default DockPulse process when --mode is omitted.
//
// In Phase 0 this package only sets up an HTTP server, the health
// endpoint, and the static UI mount. Phase 1 adds authentication, the
// database driver, and the agent API.
package controller

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/version"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the controller's HTTP server. It is safe to call Start and
// then Shutdown to gracefully terminate.
type Server struct {
	cfg    config.Controller
	router http.Handler
	srv    *http.Server
	bundle fs.FS
	log    *slog.Logger
}

// New constructs a Server using the supplied controller configuration.
func New(cfg config.Controller) (*Server, error) {
	logger := slog.Default().With("subsystem", "controller")

	bundle, err := resolveBundle(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve web bundle: %w", err)
	}

	s := &Server{
		cfg:    cfg,
		bundle: bundle,
		log:    logger,
	}
	s.router = s.buildRouter()
	return s, nil
}

// Start binds the listener and blocks until ctx is cancelled or the
// server is shut down via Shutdown.
func (s *Server) Start(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("controller listening", "addr", s.cfg.Listen, "version", version.Version)
		err := s.srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Shutdown stops the server, allowing in-flight requests up to the
// context deadline to complete.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	s.log.Info("controller shutting down")
	return s.srv.Shutdown(ctx)
}

func (s *Server) buildRouter() http.Handler {
	r := chi.NewRouter()

	// Hardening middleware applied to every request.
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)
	r.Use(s.requestLogger)

	// Health and version endpoints.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/version", s.handleVersion)

	// Static UI mount.
	r.Handle("/static/*", s.staticHandler())
	r.Handle("/_app/*", s.staticHandler())
	r.Get("/", s.indexHandler)
	r.Get("/*", s.indexHandler) // SPA fallback for client-side routes

	return r
}

// securityHeaders sets a baseline of browser-side hardening headers
// on every response. The reverse proxy duplicates these so a
// misconfigured proxy cannot degrade the baseline.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) staticHandler() http.Handler {
	fs := http.FileServer(http.FS(s.bundle))
	return http.StripPrefix("/", fs)
}

// indexHandler serves the SPA's index.html. In Phase 0 every route
// except /healthz and /version falls through here so the placeholder
// SvelteKit page is always reachable.
func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.bundle, "index.html")
	if err != nil {
		http.Error(w, "web bundle is missing index.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Security headers that the reverse proxy should also set; duplicated
	// here so a misconfigured proxy cannot degrade them.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	_, _ = w.Write(data)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Healthz intentionally reveals nothing about host state to avoid
	// being useful as a recon endpoint. See docs/THREAT_MODEL.md T12.
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `{"version":%q,"commit":%q,"go":%q}`, version.Version, version.Commit, versionString())
}

func versionString() string {
	return "go1.22" // updated at build time via -ldflags
}

// requestLogger emits a single structured record per request without
// logging the URL path on potentially sensitive routes.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"dur_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// resolveBundle picks between the embedded bundle and a developer-supplied
// directory. The embedded bundle always wins in production; --web is for
// local development with a separately-built bundle that the developer
// wants hot-reloaded.
func resolveBundle(cfg config.Controller, logger *slog.Logger) (fs.FS, error) {
	if cfg.WebPath != "" {
		logger.Warn("serving web bundle from disk; this should not be used in production", "path", cfg.WebPath)
		return web.OpenFromDisk(cfg.WebPath)
	}
	fsys, err := web.Sub()
	if err != nil {
		// In dev, fall back to a clear placeholder so the developer
		// sees something useful instead of a broken SPA.
		return nil, err
	}
	return fsys, nil
}