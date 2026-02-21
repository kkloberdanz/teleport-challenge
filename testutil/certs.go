package testutil

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kkloberdanz/teleworker/auth"
)

// CertsDir returns the absolute path to the certs/ directory at the project root.
func CertsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine testutil package location")
	}
	return filepath.Join(filepath.Dir(filepath.Dir(filename)), "certs")
}

// CACertPEM reads and returns the CA certificate PEM from certs/ca.crt.
func CACertPEM(t *testing.T) []byte {
	t.Helper()
	pem, err := os.ReadFile(filepath.Join(CertsDir(t), "ca.crt"))
	if err != nil {
		t.Fatalf("failed to read CA cert: %v", err)
	}
	return pem
}

// LoadCert loads a TLS certificate and key from certs/<name>.crt and certs/<name>.key.
func LoadCert(t *testing.T, name string) tls.Certificate {
	t.Helper()
	dir := CertsDir(t)
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(dir, name+".crt"),
		filepath.Join(dir, name+".key"),
	)
	if err != nil {
		t.Fatalf("failed to load cert %q: %v", name, err)
	}
	return cert
}

// ServerTLSConfig returns a *tls.Config for the server using certs from certs/.
func ServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	conf, err := auth.ServerTLSConfig(CACertPEM(t), LoadCert(t, "server"))
	if err != nil {
		t.Fatalf("failed to build server TLS config: %v", err)
	}
	return conf
}

// ClientTLSConfig returns a *tls.Config for a client using certs/<name>.crt/.key.
func ClientTLSConfig(t *testing.T, name string) *tls.Config {
	t.Helper()
	conf, err := auth.ClientTLSConfig(CACertPEM(t), LoadCert(t, name), "teleworker")
	if err != nil {
		t.Fatalf("failed to build client TLS config: %v", err)
	}
	return conf
}
