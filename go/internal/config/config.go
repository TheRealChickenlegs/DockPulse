// Package config centralises DockPulse's runtime configuration.
//
// Configuration is read from a single source: CLI flags. Environment
// variables may be used as defaults for individual flags in the future;
// Phase 0 keeps the surface intentionally small.
package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mode identifies which role this DockPulse process is running.
type Mode string

const (
	// ModeController runs the web UI, the agent-facing API, and the
	// database. This is the default mode.
	ModeController Mode = "controller"

	// ModeAgent runs on each Docker host. The agent reports to a
	// controller over mTLS and is responsible for local Docker
	// inventory and registry polling.
	ModeAgent Mode = "agent"
)

// Common holds configuration shared by both controller and agent modes.
type Common struct {
	Mode Mode
}

// Controller holds configuration specific to controller mode.
type Controller struct {
	Common
	Listen         string   // address to bind the controller HTTP server (no TLS)
	DBPath         string   // path to the SQLite file
	WebPath        string   // path to the static UI bundle (dev override)
	StaticFS       string   // directory served for /static/* when not using embed
	TrustedProxies []string // CIDR/IPs allowed to set X-Forwarded-For; empty disables trust
	CookieSecure   bool     // set Secure flag on session cookies (require TLS)
	AgentCADir     string   // directory holding the agent CA cert/key (created on first run)
}

// Agent holds configuration specific to agent mode.
type Agent struct {
	Common
	Name                    string // human-readable name for this agent host
	ControllerURL           string // controller base URL, e.g. https://dockpulse.example.com
	AllowInsecureController bool   // TEST ONLY: allow http:// controllers even when host is not local
	DockerHost              string // docker daemon URL, e.g. unix:///var/run/docker.sock
	DataDir                 string // directory for certs, registry creds, state
	EnrollTokenFile         string // path to a file containing the one-time enrollment token
	ControllerCAFile        string // path to the controller CA certificate (for pinning)
	RegistryPollInterval    time.Duration // how often to poll registries for image updates
}

// Config is the resolved configuration for the running process.
type Config struct {
	ControllerURL string // mode-aware convenience (empty for controller mode)
}

// String implements fmt.Stringer for log output without leaking secrets.
func (c Common) String() string {
	return fmt.Sprintf("mode=%s", c.Mode)
}

// String implements fmt.Stringer for log output without leaking secrets.
// The DB path is redacted because SQLite paths can leak host layout
// and are not useful in logs. TrustedProxies is summarised by count
// only so individual entries don't leak the operator's network shape.
func (c Controller) String() string {
	return fmt.Sprintf("%s listen=%s db=%s web=%s static=%s trusted_proxies=%d cookie_secure=%t ca_dir=%s",
		c.Common, c.Listen, redactPath(c.DBPath), redactPath(c.WebPath), c.StaticFS, len(c.TrustedProxies), c.CookieSecure, redactPath(c.AgentCADir))
}

// String implements fmt.Stringer for log output without leaking secrets.
func (a Agent) String() string {
	return fmt.Sprintf("%s name=%s controller=%s docker=%s data=%s", a.Common, a.Name, redactURL(a.ControllerURL), a.DockerHost, a.DataDir)
}

func redactURL(raw string) string {
	if i := strings.Index(raw, "@"); i > 0 {
		if j := strings.Index(raw, "://"); j > 0 && j < i {
			return raw[:j+3] + "<redacted>" + raw[i:]
		}
	}
	return raw
}

// redactPath returns just the basename of a filesystem path. The full
// path is rarely useful in logs and may leak host layout or usernames.
func redactPath(p string) string {
	if p == "" {
		return ""
	}
	if p == ":memory:" {
		return p
	}
	base := filepath.Base(p)
	if base == "." || base == "/" || base == "" {
		return "<redacted>"
	}
	return base
}

// Load parses CLI flags and returns the resolved configuration.
//
// The first positional argument determines the mode ("controller" or
// "agent"). Unknown arguments cause usage to be printed and the program
// to exit with a non-zero status.
func Load(args []string) (any, error) {
	fs := flag.NewFlagSet("dockpulse", flag.ContinueOnError)

	modeStr := fs.String("mode", string(ModeController), "Operating mode: controller or agent")
	// Listen address defaults to ":9787" - all interfaces on port
	// 9787. This is friendlier than 127.0.0.1:9787 because the
	// dashboard is reachable from the LAN without any extra flags,
	// and friendlier than 8080 because that port is commonly used
	// by other apps (Prometheus, Tomcat, Jenkins, etc.).
	listen := fs.String("listen", ":9787", "Address for the controller HTTP listener (controller mode). Use the host's LAN IP (e.g. 192.168.10.10:9787) to expose the dashboard on the LAN; leave as :9787 for all interfaces")
	dbPath := fs.String("db", "./data/dockpulse.db", "Path to the SQLite database file (controller mode)")
	webPath := fs.String("web", "", "Path to a built web bundle (overrides embedded bundle; controller mode, dev only)")
	staticDir := fs.String("static", "", "Directory served for /static/* when --web is set (controller mode)")
	trustedProxiesFlag := fs.String("trusted-proxies", os.Getenv("DOCKPULSE_TRUSTED_PROXIES"),
		"Comma-separated list of reverse-proxy IPs/CIDRs allowed to set X-Forwarded-For (controller mode). Empty disables trust.")
	cookieSecure := fs.Bool("secure-cookies", envBool("DOCKPULSE_COOKIE_SECURE", false),
		"Set the Secure attribute on session cookies (controller mode). Required in production behind TLS.")
	agentCADir := fs.String("agent-ca-dir",
		firstNonEmpty(os.Getenv("DOCKPULSE_AGENT_CA_DIR"), filepath.Join(filepath.Dir(*dbPath), "agent-ca")),
		"Directory holding the agent CA cert and key (controller mode). Created on first run.")

	// Agent-only
	name := fs.String("name", "", "Friendly name for this agent host (agent mode)")
	controllerURL := fs.String("controller", "", "Controller base URL, e.g. https://dockpulse.example.com (agent mode)")
	// Allow insecure (http://) controllers when the host is
	// obviously local (loopback, RFC1918, link-local, .local,
	// .lan). For everything else https:// is required. The
	// --allow-insecure-controller flag bypasses the check
	// entirely (test only - emits a loud warning).
	allowInsecureController := fs.Bool("allow-insecure-controller", envBool("DOCKPULSE_ALLOW_INSECURE_CONTROLLER", false),
		"TEST ONLY: permit --controller to use http:// even when the host is not a known local address. Loud warning.")
	dockerHost := fs.String("docker", "unix:///var/run/docker.sock", "Docker daemon URL (agent mode)")
	dataDir := fs.String("data", "./data", "Directory for agent state (agent mode)")
	tokenFile := fs.String("enroll-token-file", "", "Path to a file containing the one-time enrollment token (agent mode)")
	controllerCA := fs.String("controller-ca", "", "Path to the controller CA cert for fingerprint pinning (agent mode)")
	registryPoll := fs.Duration("registry-poll", time.Hour, "How often to poll registries for image updates (agent mode). Use a small value like 30s to test.")

	showVersion := fs.Bool("version", false, "Print version and exit")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if *showVersion {
		fmt.Println(VersionString())
		os.Exit(0)
	}

	mode := Mode(strings.ToLower(strings.TrimSpace(*modeStr)))
	switch mode {
	case ModeController:
		return Controller{
			Common:         Common{Mode: mode},
			Listen:         *listen,
			DBPath:         *dbPath,
			WebPath:        *webPath,
			StaticFS:       *staticDir,
			TrustedProxies: splitCSV(*trustedProxiesFlag),
			CookieSecure:   *cookieSecure,
			AgentCADir:     *agentCADir,
		}, nil
	case ModeAgent:
		if *controllerURL == "" {
			return nil, fmt.Errorf("--controller is required in agent mode")
		}
		// HTTPS-vs-HTTP validation is done in the agent's validate()
		// so that local hosts (loopback, RFC1918, link-local, .local,
		// .lan) can be reached over plain HTTP without the
		// --allow-insecure-controller flag. We only check the scheme
		// is recognised here.
		parsed, perr := url.Parse(*controllerURL)
		if perr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("--controller must be an http:// or https:// URL")
		}
		if abs, err := filepath.Abs(*dataDir); err == nil {
			if strings.HasPrefix(abs, "/proc") || strings.HasPrefix(abs, "/sys") {
				return nil, fmt.Errorf("refusing to use %s as data dir", abs)
			}
		}
		return Agent{
			Common:                  Common{Mode: mode},
			Name:                    *name,
			ControllerURL:           *controllerURL,
			AllowInsecureController: *allowInsecureController,
			DockerHost:              *dockerHost,
			DataDir:                 *dataDir,
			EnrollTokenFile:         *tokenFile,
			ControllerCAFile:        *controllerCA,
			RegistryPollInterval:    *registryPoll,
		}, nil
	default:
		return nil, fmt.Errorf("unknown mode %q (expected controller or agent)", *modeStr)
	}
}

// VersionString is provided as a thin wrapper so the version package does
// not need to be imported by every caller. Defined here to avoid an import
// cycle in Phase 1.
func VersionString() string {
	return "DockPulse dev"
}

// splitCSV trims whitespace and drops empty entries. It is intentionally
// permissive — invalid CIDR entries are caught later by the caller when
// the addresses are parsed at the network layer.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envBool parses a boolean from an environment variable. Accepts
// 1, t, true, yes, on (case-insensitive) as true; everything else
// (including empty) is false.
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
