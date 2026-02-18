package auth_test

import (
	"crypto/tls"
	"testing"

	"github.com/kkloberdanz/teleworker/auth"
	"github.com/kkloberdanz/teleworker/testutil"
)

func TestServerTLSConfig(t *testing.T) {
	conf, err := auth.ServerTLSConfig(testutil.CACertPEM(t), testutil.LoadCert(t, "server"))
	if err != nil {
		t.Fatalf("failed to load server tls config: %v", err)
	}

	if conf.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected RequireAndVerifyClientCert, got %v", conf.ClientAuth)
	}
	if conf.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3, got %v", conf.MinVersion)
	}
}

func TestClientTLSConfig(t *testing.T) {
	conf, err := auth.ClientTLSConfig(testutil.CACertPEM(t), testutil.LoadCert(t, "alice"), "myserver")
	if err != nil {
		t.Fatalf("failed to load client tls config: %v", err)
	}

	if conf.ServerName != "myserver" {
		t.Fatalf("expected ServerName %q, got %q", "myserver", conf.ServerName)
	}
	if conf.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3, got %v", conf.MinVersion)
	}
}

func TestServerTLSConfigInvalidCA(t *testing.T) {
	cert := tls.Certificate{}
	_, err := auth.ServerTLSConfig([]byte("not-a-cert"), cert)
	if err == nil {
		t.Fatal("expected error for invalid CA PEM, got nil")
	}
}

func TestClientTLSConfigInvalidCA(t *testing.T) {
	cert := tls.Certificate{}
	_, err := auth.ClientTLSConfig([]byte("not-a-cert"), cert, "server")
	if err == nil {
		t.Fatal("expected error for invalid CA PEM, got nil")
	}
}
