package auth_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"testing"

	"go.uber.org/goleak"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/kkloberdanz/teleworker/auth"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestIdentityFromContext(t *testing.T) {
	ctx := peerContextFromFile(t, "../certs/alice.crt")

	id, err := auth.IdentityFromContext(ctx)
	if err != nil {
		t.Fatalf("failed to load identity: %v", err)
	}
	if id.Username != "alice" {
		t.Fatalf("expected username %q, got %q", "alice", id.Username)
	}
	if id.Role != "client" {
		t.Fatalf("expected role %q, got %q", "client", id.Role)
	}
}

func TestIdentityFromContextAdmin(t *testing.T) {
	ctx := peerContextFromFile(t, "../certs/admin.crt")

	id, err := auth.IdentityFromContext(ctx)
	if err != nil {
		t.Fatalf("failed to load identity: %v", err)
	}
	if !id.IsAdmin() {
		t.Fatal("expected admin, got non-admin")
	}
}

func TestIdentityFromContextNoPeer(t *testing.T) {
	_, err := auth.IdentityFromContext(t.Context())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestIdentityFromContextNoTLS(t *testing.T) {
	// Peer with nil AuthInfo (not TLS).
	ctx := peer.NewContext(t.Context(), &peer.Peer{})
	_, err := auth.IdentityFromContext(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestIdentityFromContextEmptyChains(t *testing.T) {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: nil,
		},
	}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: tlsInfo})

	_, err := auth.IdentityFromContext(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestIdentityFromContextEmptyOU(t *testing.T) {
	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: "alice",
		},
	}
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{cert}},
		},
	}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: tlsInfo})

	_, err := auth.IdentityFromContext(ctx)
	if err == nil {
		t.Fatal("expected error for empty OU, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestIdentityFromContextUnrecognizedRole(t *testing.T) {
	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:         "alice",
			OrganizationalUnit: []string{"superuser"},
		},
	}
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{cert}},
		},
	}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: tlsInfo})

	_, err := auth.IdentityFromContext(ctx)
	if err == nil {
		t.Fatal("expected error for unrecognized role, got nil")
	}
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestIsAdmin(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{"admin", true},
		{"client", false},
		{"", false},
	}
	for _, tt := range tests {
		id := auth.Identity{Username: "test", Role: tt.role}
		if got := id.IsAdmin(); got != tt.want {
			t.Errorf("Identity{Role: %q}.IsAdmin() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

// peerContextFromFile loads a PEM certificate from path and returns a context
// with a TLS peer containing that certificate in the verified chain.
func peerContextFromFile(t *testing.T, path string) context.Context {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read certificate %s: %v", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("no PEM block found in %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate %s: %v", path, err)
	}

	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{cert}},
		},
	}
	return peer.NewContext(t.Context(), &peer.Peer{AuthInfo: tlsInfo})
}
