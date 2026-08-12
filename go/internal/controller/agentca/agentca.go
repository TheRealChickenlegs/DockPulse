// Package agentca owns the controller's internal certificate
// authority used to mint mTLS client certificates for agents.
//
// The CA is generated on first run if no key file exists, and
// persisted under the controller's data directory. Agents pin
// the SHA-256 fingerprint of the CA at enrollment time so a
// later MITM with a different CA cannot impersonate the
// controller.
package agentca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// CA is the controller's persistent internal certificate authority.
type CA struct {
	Cert    *x509.Certificate
	Key     *ecdsa.PrivateKey
	CertPEM []byte
}

// LoadOrCreate returns the on-disk CA, generating it if it does
// not yet exist. The CA key is stored unencrypted in the data
// directory — the controller host is the trust boundary; an
// attacker with read access to the data dir already has full
// control.
func LoadOrCreate(dataDir string) (*CA, error) {
	certPath := filepath.Join(dataDir, "agent-ca.crt")
	keyPath := filepath.Join(dataDir, "agent-ca.key")

	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			return parsePEM(certPEM, keyPEM)
		}
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("agentca: mkdir: %w", err)
	}

	ca, err := generate()
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(certPath, ca.CertPEM, 0o644); err != nil {
		return nil, fmt.Errorf("agentca: write cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(ca.Key)
	if err != nil {
		return nil, fmt.Errorf("agentca: marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("agentca: write key: %w", err)
	}
	return ca, nil
}

func parsePEM(certPEM, keyPEM []byte) (*CA, error) {
	cb, _ := pem.Decode(certPEM)
	if cb == nil || cb.Type != "CERTIFICATE" {
		return nil, errors.New("agentca: invalid CA cert PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse cert: %w", err)
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, errors.New("agentca: invalid CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse key: %w", err)
	}
	return &CA{Cert: cert, Key: key, CertPEM: certPEM}, nil
}

func generate() (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("agentca: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("agentca: serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "DockPulse Agent CA",
			Organization: []string{"DockPulse"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("agentca: create: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &CA{Cert: cert, Key: key, CertPEM: certPEM}, nil
}

// Fingerprint returns the hex-encoded SHA-256 fingerprint of the
// CA certificate. This is what agents pin at enrollment time.
func (c *CA) Fingerprint() string {
	sum := sha256.Sum256(c.Cert.Raw)
	return hex.EncodeToString(sum[:])
}

// IssuedCert is the response of IssueClient: a freshly minted
// client certificate plus the CA certificate for the agent to
// chain to.
type IssuedCert struct {
	ClientPEM []byte
	CACertPEM []byte
	NotAfter  time.Time
	Fingerprint string
}

// IssueClient signs a client certificate for an agent. The agent
// generates its own key pair locally and sends a CSR; the
// controller never holds the agent's private key.
func (c *CA) IssueClient(csrPEM []byte, commonName string, ttl time.Duration) (*IssuedCert, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("agentca: not a CSR")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse csr: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("agentca: csr signature invalid: %w", err)
	}
	if csr.Subject.CommonName == "" {
		csr.Subject.CommonName = commonName
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("agentca: serial: %w", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   csr.Subject.CommonName,
			Organization: csr.Subject.Organization,
		},
		NotBefore:             now,
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              csr.DNSNames,
		IPAddresses:           csr.IPAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Cert, csr.PublicKey, c.Key)
	if err != nil {
		return nil, fmt.Errorf("agentca: sign: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("agentca: parse issued: %w", err)
	}
	clientPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	sum := sha256.Sum256(cert.Raw)
	return &IssuedCert{
		ClientPEM:   clientPEM,
		CACertPEM:   c.CertPEM,
		NotAfter:    cert.NotAfter,
		Fingerprint: hex.EncodeToString(sum[:]),
	}, nil
}
