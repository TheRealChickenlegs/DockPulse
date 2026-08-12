package agentca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if ca.Cert.Subject.CommonName != "DockPulse Agent CA" {
		t.Fatalf("unexpected CN: %q", ca.Cert.Subject.CommonName)
	}
	if len(ca.CertPEM) == 0 {
		t.Fatal("expected non-empty CertPEM")
	}
	fp := ca.Fingerprint()
	if len(fp) != 64 {
		t.Fatalf("expected 64-char hex SHA-256, got %d chars", len(fp))
	}

	ca2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if ca2.Fingerprint() != fp {
		t.Fatal("fingerprint changed across loads")
	}

	if _, err := os.ReadFile(filepath.Join(dir, "agent-ca.crt")); err != nil {
		t.Fatal("cert file missing")
	}
	if _, err := os.ReadFile(filepath.Join(dir, "agent-ca.key")); err != nil {
		t.Fatal("key file missing")
	}
}

func TestIssueClient(t *testing.T) {
	dir := t.TempDir()
	ca, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("ca: %v", err)
	}

	csrKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("agent key: %v", err)
	}
	csrTmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "agent-test",
			Organization: []string{"DockPulse"},
		},
		DNSNames: []string{"server-a.local"},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTmpl, csrKey)
	if err != nil {
		t.Fatalf("csr: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	issued, err := ca.IssueClient(csrPEM, "agent-test", 24*60*60*1e9)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.Fingerprint == ca.Fingerprint() {
		t.Fatal("issued cert fingerprint must differ from CA")
	}
	if len(issued.ClientPEM) == 0 || len(issued.CACertPEM) == 0 {
		t.Fatal("expected both client and CA PEM")
	}
}

func TestIssueClientRejectsBadCSR(t *testing.T) {
	dir := t.TempDir()
	ca, _ := LoadOrCreate(dir)

	if _, err := ca.IssueClient([]byte("not a pem"), "x", 0); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}
