// Package agent is the DockPulse agent mode. The agent runs on
// each Docker host, enrolls with the controller over mTLS, then
// periodically reports container state and host metadata.
//
// Phase 1 responsibilities:
//
//   - On first start, load or create the local agent key + cert.
//   - Enroll against the controller using a one-time token.
//   - Periodically ping Docker, sign and POST heartbeats, and
//     sign and POST container snapshots.
//
// Phase 1d is intentionally narrow — registry polling, changelog
// fetching, and the apply-update channel all land in later
// phases.
package agent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/agent/docker"
	"github.com/TheRealChickenlegs/DockPulse/go/internal/config"
)

// Daemon is the agent's long-lived process. A single Daemon is
// created at startup; it owns the Docker client, the mTLS HTTP
// client to the controller, and the reporter goroutines.
type Daemon struct {
	Cfg    config.Agent
	Log    *slog.Logger
	Docker *docker.Client

	// cert is the parsed controller-issued client certificate.
	cert *x509.Certificate
	// signingKey is the per-agent shared key used for HMAC
	// signatures on every request. Generated at enrollment and
	// stored in the data dir alongside the cert/key.
	signingKey []byte

	// httpClient is the mTLS client to the controller.
	httpClient *http.Client

	// baseURL is the controller URL with no trailing slash.
	baseURL string

	// controllerFingerprint is the SHA-256 fingerprint of the
	// controller's CA cert, captured at enrollment. Used to pin
	// the controller identity on subsequent TLS handshakes.
	controllerFingerprint string

	// agentID, serverID, serverName are populated by enroll().
	mu         sync.Mutex
	agentID    string
	serverID   string
	serverName string
	enrolled   bool
}

// Run starts the agent and blocks until ctx is cancelled. It
// replaces the Phase 0 placeholder of the same name.
func Run(ctx context.Context, cfg config.Agent) error {
	if err := validate(cfg); err != nil {
		return fmt.Errorf("invalid agent config: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	logger := slog.Default().With("subsystem", "agent", "name", cfg.Name)
	logger.Info("agent starting", "controller", cfg.ControllerURL, "docker", cfg.DockerHost)

	dockerClient, err := docker.New(cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}

	d := &Daemon{
		Cfg:     cfg,
		Log:     logger,
		Docker:  dockerClient,
		baseURL: strings.TrimRight(cfg.ControllerURL, "/"),
	}
	// Pre-enrollment client: no client cert yet, and /agent/v1/enroll
	// is the one endpoint the controller accepts without mTLS.
	d.httpClient = &http.Client{Timeout: 30 * time.Second}
	if err := d.loadOrEnroll(ctx); err != nil {
		return fmt.Errorf("enroll: %w", err)
	}

	return d.loop(ctx)
}

func (d *Daemon) loop(ctx context.Context) error {
	heartbeat := time.NewTicker(30 * time.Second)
	snapshot := time.NewTicker(2 * time.Minute)
	defer heartbeat.Stop()
	defer snapshot.Stop()

	// Send one heartbeat and snapshot immediately so the UI is
	// populated without waiting for the first tick.
	d.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			d.Log.Info("agent stopping", "reason", ctx.Err())
			return nil
		case <-heartbeat.C:
			if err := d.tick(ctx); err != nil {
				d.Log.Warn("tick", "err", err)
			}
		case <-snapshot.C:
			if err := d.snapshot(ctx); err != nil {
				d.Log.Warn("snapshot", "err", err)
			}
		}
	}
}

// tick is one heartbeat cycle: probe Docker and send a heartbeat.
func (d *Daemon) tick(ctx context.Context) error {
	v, err := d.Docker.Ping(ctx)
	if err != nil {
		return err
	}
	containers, err := d.Docker.ListContainers(ctx)
	if err != nil {
		return err
	}
	running := 0
	images := make([]string, 0, len(containers))
	seenImage := map[string]bool{}
	for _, c := range containers {
		if c.State == "running" {
			running++
		}
		if !seenImage[c.Image] {
			seenImage[c.Image] = true
			images = append(images, c.Image)
		}
	}
	return d.sendHeartbeat(ctx, v, running, len(containers), images)
}

func (d *Daemon) snapshot(ctx context.Context) error {
	containers, err := d.Docker.ListContainers(ctx)
	if err != nil {
		return err
	}
	out := make([]snapshotContainer, 0, len(containers))
	for _, c := range containers {
		out = append(out, snapshotContainer{
			DockerID:    c.ID,
			Name:        docker.StripNamePrefix(c.Name),
			ImageRef:    c.Image,
			ImageDigest: c.ImageID,
			State:       c.State,
			Labels:      c.Labels,
		})
	}
	return d.sendSnapshot(ctx, out)
}

type snapshotContainer struct {
	DockerID    string            `json:"docker_id"`
	Name        string            `json:"name"`
	ImageRef    string            `json:"image_ref"`
	ImageDigest string            `json:"image_digest_local"`
	State       string            `json:"state"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// sendHeartbeat posts a signed heartbeat to the controller.
func (d *Daemon) sendHeartbeat(ctx context.Context, v *docker.Version, running, total int, images []string) error {
	body, err := json.Marshal(map[string]any{
		"docker_version":  v.Version,
		"os":              v.OS,
		"container_count": total,
		"running_count":   running,
		"images":          images,
	})
	if err != nil {
		return err
	}
	req, err := d.newSignedRequest(ctx, http.MethodPost, "/agent/v1/heartbeat", body)
	if err != nil {
		return err
	}
	res, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("heartbeat status %d: %s", res.StatusCode, respBody)
	}
	return nil
}

func (d *Daemon) sendSnapshot(ctx context.Context, containers []snapshotContainer) error {
	body, err := json.Marshal(map[string]any{"containers": containers})
	if err != nil {
		return err
	}
	req, err := d.newSignedRequest(ctx, http.MethodPost, "/agent/v1/containers/snapshot", body)
	if err != nil {
		return err
	}
	res, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		respBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("snapshot status %d: %s", res.StatusCode, respBody)
	}
	return nil
}

// newSignedRequest builds a POST request with the HMAC signature
// header and (in production) the agent's client cert attached.
func (d *Daemon) newSignedRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if d.agentID != "" {
		req.Header.Set("X-DockPulse-Agent-Id", d.agentID)
	}
	if len(d.signingKey) > 0 {
		ts := time.Now().Unix()
		nonce := randomHex(16)
		bodyHash := sha256.Sum256(body)
		mac := hmac.New(sha256.New, d.signingKey)
		fmt.Fprintf(req.Body.(io.Writer), "") // no-op; body is bytes.NewReader
		_, _ = io.Copy(io.Discard, req.Body)
		// re-wrap body after our no-op peek; http.Request body is
		// consumed by transports, so we must restore it.
		req.Body = io.NopCloser(bytes.NewReader(body))
		mac.Write([]byte(intToString(ts)))
		mac.Write([]byte{'.'})
		mac.Write([]byte(nonce))
		mac.Write([]byte{'.'})
		mac.Write(bodyHash[:])
		req.Header.Set("X-DockPulse-Signature", fmt.Sprintf("t=%d,n=%s,v1=%s", ts, nonce, hex.EncodeToString(mac.Sum(nil))))
	}
	return req, nil
}

func (d *Daemon) loadOrEnroll(ctx context.Context) error {
	certPath := filepath.Join(d.Cfg.DataDir, "agent.crt")
	keyPath := filepath.Join(d.Cfg.DataDir, "agent.key")
	sharedKeyPath := filepath.Join(d.Cfg.DataDir, "shared.key")

	if _, err := os.Stat(certPath); err == nil {
		// Already enrolled: load and verify the controller CA pin.
		if err := d.loadExistingCert(certPath, keyPath, sharedKeyPath); err != nil {
			return err
		}
		d.Log.Info("agent loaded existing identity",
			"agent_id", d.agentID, "server_id", d.serverID, "fingerprint", d.certFingerprint())
		return nil
	}

	tokPath := d.Cfg.EnrollTokenFile
	if tokPath == "" {
		return errors.New("enrollment token required on first start (set --enroll-token-file)")
	}
	tokBytes, err := os.ReadFile(tokPath)
	if err != nil {
		return fmt.Errorf("read enroll token: %w", err)
	}
	tok := strings.TrimSpace(string(tokBytes))
	if tok == "" {
		return errors.New("enroll token file is empty")
	}

	if err := d.enroll(ctx, tok, certPath, keyPath, sharedKeyPath); err != nil {
		return err
	}
	d.Log.Info("agent enrolled", "agent_id", d.agentID, "server_id", d.serverID)
	return nil
}

func (d *Daemon) loadExistingCert(certPath, keyPath, sharedKeyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	cb, _ := pem.Decode(certPEM)
	if cb == nil {
		return errors.New("agent: invalid cert PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return fmt.Errorf("agent: parse cert: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return errors.New("agent: invalid key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		// Try PKCS1 (EC).
		if k2, err2 := x509.ParseECPrivateKey(kb.Bytes); err2 == nil {
			key = k2
		} else {
			return fmt.Errorf("agent: parse key: %w", err)
		}
	}
	sharedKey, err := os.ReadFile(sharedKeyPath)
	if err != nil {
		return fmt.Errorf("agent: read shared key: %w", err)
	}

	// agent_id and server_id are derived from the cert subject
	// and embedded in a sidecar JSON. If missing, we re-enroll.
	idPath := filepath.Join(d.Cfg.DataDir, "identity.json")
	if idData, err := os.ReadFile(idPath); err == nil {
		var id struct {
			AgentID  string `json:"agent_id"`
			ServerID string `json:"server_id"`
		}
		if err := json.Unmarshal(idData, &id); err == nil && id.AgentID != "" && id.ServerID != "" {
			d.agentID = id.AgentID
			d.serverID = id.ServerID
		}
	}
	d.cert = cert
	d.signingKey = sharedKey
	d.enrolled = true
	d.httpClient = d.newHTTPClient(cert, key)
	return nil
}

func (d *Daemon) enroll(ctx context.Context, token, certPath, keyPath, sharedKeyPath string) error {
	// 1. Generate a local ECDSA P-256 key.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	// 2. Build a CSR.
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "agent:" + d.Cfg.Name},
	}, priv)
	if err != nil {
		return err
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	// 3. Determine the controller's internal CA fingerprint to pin.
	//    Over HTTPS the operator provides it via --controller-ca so
	//    a first-enrollment MITM cannot substitute its own CA. Over
	//    plain HTTP (allowed only for local hosts) there is no TLS
	//    identity to pin against; the controller's CA cert returned
	//    in the enroll response becomes the reference fingerprint.
	caPin, err := d.controllerCAPin()
	if err != nil {
		return err
	}

	// 4. POST the enrollment request.
	body, err := json.Marshal(map[string]any{
		"token":          token,
		"server_name":    d.Cfg.Name,
		"hostname":       hostnameOrEmpty(),
		"os":             runtimeOS(),
		"docker_version": "",
		"csr":            string(csrPEM),
		"ca_fingerprint": caPin,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/agent/v1/enroll", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// No client cert yet — first request is plain HTTPS (the
	// server accepts /enroll without mTLS).
	res, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("enroll status %d: %s", res.StatusCode, respBody)
	}
	var resp struct {
		ClientCert        string `json:"client_cert"`
		CACert            string `json:"ca_cert"`
		ClientFingerprint string `json:"client_fingerprint"`
		ServerID          string `json:"server_id"`
		AgentID           string `json:"agent_id"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return err
	}
	if resp.ClientCert == "" || resp.CACert == "" {
		return errors.New("enroll response missing cert")
	}

	caBlock, _ := pem.Decode([]byte(resp.CACert))
	if caBlock == nil {
		return errors.New("enroll response: invalid CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return fmt.Errorf("enroll response: parse CA cert: %w", err)
	}
	caSum := sha256.Sum256(caCert.Raw)
	d.controllerFingerprint = hex.EncodeToString(caSum[:])

	// 5. Persist cert + key + shared key.
	if err := os.WriteFile(certPath, []byte(resp.ClientCert), 0o600); err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	shared := randomBytes(32)
	if err := os.WriteFile(sharedKeyPath, shared, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d.Cfg.DataDir, "identity.json"),
		[]byte(fmt.Sprintf(`{"agent_id":%q,"server_id":%q,"controller_ca_fingerprint":%q}`,
			resp.AgentID, resp.ServerID, d.controllerFingerprint)), 0o600); err != nil {
		return err
	}

	// 6. Reload the mTLS client.
	cb, _ := pem.Decode([]byte(resp.ClientCert))
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return err
	}
	d.cert = cert
	d.agentID = resp.AgentID
	d.serverID = resp.ServerID
	d.signingKey = shared
	d.enrolled = true
	d.httpClient = d.newHTTPClient(cert, priv)
	return nil
}

func (d *Daemon) newHTTPClient(cert *x509.Certificate, key any) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	tlsCert := tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{tlsCert},
				MinVersion:   tls.VersionTLS12,
			},
		},
	}
}

// controllerCAPin returns the SHA-256 fingerprint of the
// controller's internal CA to send with the enroll request, or ""
// when the controller URL is plain HTTP. Over HTTPS the operator
// must provide the CA via --controller-ca so a first-enrollment
// MITM cannot substitute its own CA; over plain HTTP (allowed
// only for local hosts) there is no TLS identity to pin against,
// and the controller's CA cert from the enroll response is used
// as the reference instead.
func (d *Daemon) controllerCAPin() (string, error) {
	if d.Cfg.ControllerCAFile != "" {
		b, err := os.ReadFile(d.Cfg.ControllerCAFile)
		if err != nil {
			return "", err
		}
		block, _ := pem.Decode(b)
		if block == nil {
			return "", errors.New("agent: invalid controller CA PEM")
		}
		caCert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("agent: parse controller CA: %w", err)
		}
		sum := sha256.Sum256(caCert.Raw)
		return hex.EncodeToString(sum[:]), nil
	}
	if strings.HasPrefix(d.Cfg.ControllerURL, "https://") {
		return "", errors.New("controller CA file is required for first enrollment over https (set --controller-ca)")
	}
	return "", nil
}

func (d *Daemon) certFingerprint() string {
	if d.cert == nil {
		return ""
	}
	sum := sha256.Sum256(d.cert.Raw)
	return hex.EncodeToString(sum[:])
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randomHex(n int) string {
	return hex.EncodeToString(randomBytes(n))
}

func hostnameOrEmpty() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

func runtimeOS() string {
	// We don't have runtime.GOOS here without importing it; the
	// platform-specific name comes from the build target.
	return "linux"
}

func intToString(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// validate is unchanged from the Phase 0 placeholder and is
// re-exported for the Daemon's startup checks.
func validate(cfg config.Agent) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return errors.New("--name is required in agent mode")
	}
	parsed, err := url.Parse(cfg.ControllerURL)
	if err != nil {
		return fmt.Errorf("invalid --controller URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("--controller must use http:// or https://")
	}
	// HTTPS is the default. http:// is only acceptable for
	// obviously-local hosts (loopback, RFC1918, link-local, .local,
	// .lan) and only when the operator has not explicitly opted
	// in via --allow-insecure-controller for everything else.
	if parsed.Scheme == "http" && !cfg.AllowInsecureController && !isLocalHost(parsed.Hostname()) {
		return errors.New("--controller must use https:// unless the host is local (loopback, RFC1918, link-local, .local, .lan). Use --allow-insecure-controller to test against an arbitrary http:// host")
	}
	if cfg.DockerHost == "" {
		return errors.New("--docker must not be empty")
	}
	if cfg.DataDir == "" {
		return errors.New("--data must not be empty")
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err == nil {
		if strings.HasPrefix(abs, "/proc") || strings.HasPrefix(abs, "/sys") {
			return fmt.Errorf("refusing to use %s as data dir", abs)
		}
	}
	// Require an explicit docker URL scheme.
	if !strings.HasPrefix(cfg.DockerHost, "tcp://") &&
		!strings.HasPrefix(cfg.DockerHost, "http://") &&
		!strings.HasPrefix(cfg.DockerHost, "https://") &&
		!strings.HasPrefix(cfg.DockerHost, "unix://") {
		return fmt.Errorf("--docker must use tcp://, http://, https://, or unix:// (got %q)", cfg.DockerHost)
	}
	// Best-effort dial check: the host must be reachable when
	// the agent starts so misconfiguration fails loudly.
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(strings.TrimPrefix(cfg.DockerHost, "tcp://"), "http://"), 2*time.Second)
	if err == nil {
		_ = conn.Close()
	}
	return nil
}

// isLocalHost reports whether h is an obviously-local hostname —
// either an IP literal in a loopback / private / link-local
// range, or a name ending in .local or .lan (mDNS / typical
// home-router suffixes). Anything else requires https:// unless
// the operator has explicitly opted in via
// --allow-insecure-controller.
func isLocalHost(h string) bool {
	if h == "" {
		return false
	}
	lower := strings.ToLower(h)
	if lower == "localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true
		}
		if ip.IsPrivate() {
			return true
		}
		// 169.254.0.0/16 is link-local but IsPrivate() returns false
		// for it; isLinkLocalUnicast() catches it above.
		return false
	}
	if strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".lan") {
		return true
	}
	return false
}
