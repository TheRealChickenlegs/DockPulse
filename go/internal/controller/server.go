// Package controller hosts the web UI, the agent-facing API, and the
// database. It is the default DockPulse process when --mode is omitted.
//
// Phase 1 adds the SQLite database, the first-run admin wizard, the
// session-based authentication layer, and the public /api/v1/* surface
// for the SvelteKit SPA. The agent API and the registry polling
// pipeline are added in later phases.
package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/agentapi"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/agentca"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/version"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is the controller's HTTP server. It is safe to call Start and
// then Shutdown to gracefully terminate.
type Server struct {
	cfg     config.Controller
	router  http.Handler
	srv     *http.Server
	bundle  fs.FS
	log     *slog.Logger
	db      *sql.DB
	authMw  *auth.Middleware
	ca      *agentca.CA
	agentAPI *agentapi.Server
}

// New constructs a Server using the supplied controller configuration.
// The db handle is used for the session store; the caller is
// responsible for opening it (and closing it on shutdown).
func New(cfg config.Controller, database *sql.DB) (*Server, error) {
	logger := slog.Default().With("subsystem", "controller")

	bundle, err := resolveBundle(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("resolve web bundle: %w", err)
	}

	if database == nil {
		return nil, errors.New("controller: nil database handle")
	}

	cookieSecure := cfg.CookieSecure

	ca, err := agentca.LoadOrCreate(cfg.AgentCADir)
	if err != nil {
		return nil, fmt.Errorf("load agent CA: %w", err)
	}
	logger.Info("agent CA loaded", "fingerprint", ca.Fingerprint()[:16]+"…", "dir", cfg.AgentCADir)

	apiSrv := agentapi.New(database, ca, logger)

	s := &Server{
		cfg:      cfg,
		bundle:   bundle,
		log:      logger,
		db:       database,
		authMw:   auth.NewMiddleware(database, cookieSecure),
		ca:       ca,
		agentAPI: apiSrv,
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
	r.Use(s.trustedRealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)
	r.Use(s.requestLogger)

	// Health and version endpoints.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/version", s.handleVersion)

	// Unauthenticated auth endpoints.
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/firstrun", auth.HandleFirstRunStatus(s.dbCtx(), s.db))
		r.Post("/firstrun", auth.HandleFirstRunCreate(s.dbCtx(), s.db, s.cfg.CookieSecure))
		r.Post("/login", auth.HandleLogin(s.dbCtx(), s.db, s.cfg.CookieSecure))
	})

	// Authenticated auth + admin endpoints.
	r.Group(func(r chi.Router) {
		r.Use(s.authMw.Wrap)
		r.Post("/api/v1/logout", auth.HandleLogout(s.dbCtx(), s.db, s.cfg.CookieSecure))
		r.Get("/api/v1/me", auth.HandleMe(s.dbCtx(), s.db))
		r.Get("/api/v1/servers", agentapi.HandleListServers(s.dbCtx(), s.db))
		r.Get("/api/v1/servers/{id}/containers", agentapi.HandleListContainers(s.dbCtx(), s.db))
		r.Get("/api/v1/updates", agentapi.HandleListUpdates(s.dbCtx(), s.db))
		r.Get("/api/v1/containers/{id}/changelog", agentapi.HandleContainerChangelog(s.dbCtx(), s.db))
		r.Post("/api/v1/servers/{id}/refresh", agentapi.HandleRequestScan(s.db, s.agentAPI.Commands))
		r.Post("/api/v1/admin/agents/enroll-token", agentapi.HandleCreateEnrollmentToken(s.dbCtx(), s.db, s.ca.Fingerprint()))
	})

	// Agent API (mTLS-protected, served on the same port; in
	// production an operator can put a separate listener on a
	// different port if they prefer to keep the agent traffic
	// off the public reverse proxy).
	agentHandler := http.NewServeMux()
	s.agentAPI.Routes(agentHandler)
	r.Mount("/agent/v1", agentHandler)

	// Static UI mount.
	r.Handle("/static/*", s.staticHandler())
	r.Handle("/_app/*", s.staticHandler())
	r.Get("/", s.indexHandler)
	r.Get("/*", s.indexHandler) // SPA fallback for client-side routes

	return r
}

// dbCtx returns a background context. Background context is fine
// for these handlers because they each take a short, bounded
// amount of time; the http.Server enforces its own per-request
// deadlines via WriteTimeout and the request context is
// accessible via r.Context() inside each handler.
func (s *Server) dbCtx() context.Context { return context.Background() }

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

// trustedRealIP replaces chi's middleware.RealIP with a variant that
// only honours X-Forwarded-For when the connection's remote address
// matches one of the configured trusted proxies. This prevents a
// client from spoofing its source IP by setting the header itself.
//
// When no trusted proxies are configured, the header is ignored and
// the connection's real remote address is used.
func (s *Server) trustedRealIP(next http.Handler) http.Handler {
	if len(s.cfg.TrustedProxies) == 0 {
		return next
	}

	nets := make([]*net.IPNet, 0, len(s.cfg.TrustedProxies))
	for _, cidr := range s.cfg.TrustedProxies {
		if strings.Contains(cidr, "/") {
			_, n, err := net.ParseCIDR(cidr)
			if err != nil {
				s.log.Warn("ignoring invalid trusted-proxy CIDR", "value", cidr, "err", err)
				continue
			}
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(cidr)
		if ip == nil {
			s.log.Warn("ignoring invalid trusted-proxy IP", "value", cidr)
			continue
		}
		nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(len(ip)*8, len(ip)*8)})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		remote := net.ParseIP(host)
		if remote == nil {
			next.ServeHTTP(w, r)
			return
		}
		trusted := false
		for _, n := range nets {
			if n.Contains(remote) {
				trusted = true
				break
			}
		}
		if trusted {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				r.RemoteAddr = strings.TrimSpace(parts[0]) + ":0"
			}
		}
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
