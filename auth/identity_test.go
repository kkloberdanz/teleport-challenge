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

func TestUnaryInterceptor(t *testing.T) {
	ctx := peerContextFromFile(t, "../certs/alice.crt")

	handler := func(ctx context.Context, req any) (any, error) {
		id, err := auth.FromContext(ctx)
		if err != nil {
			return nil, err
		}
		if id.Username != "alice" {
			t.Errorf("expected username %q, got %q", "alice", id.Username)
		}
		if id.Role != "client" {
			t.Errorf("expected role %q, got %q", "client", id.Role)
		}
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnaryInterceptorAdmin(t *testing.T) {
	ctx := peerContextFromFile(t, "../certs/admin.crt")

	handler := func(ctx context.Context, req any) (any, error) {
		id, err := auth.FromContext(ctx)
		if err != nil {
			return nil, err
		}
		if !id.IsAdmin() {
			t.Error("expected admin, got non-admin")
		}
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnaryInterceptorNoPeer(t *testing.T) {
	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(t.Context(), nil, nil, handler)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestUnaryInterceptorNoTLS(t *testing.T) {
	ctx := peer.NewContext(t.Context(), &peer.Peer{})
	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestUnaryInterceptorEmptyChains(t *testing.T) {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: nil,
		},
	}
	ctx := peer.NewContext(t.Context(), &peer.Peer{AuthInfo: tlsInfo})
	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestUnaryInterceptorEmptyOU(t *testing.T) {
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
	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestUnaryInterceptorUnrecognizedRole(t *testing.T) {
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
	handler := func(ctx context.Context, req any) (any, error) {
		t.Fatal("handler should not be called")
		return nil, nil
	}

	_, err := auth.UnaryInterceptor(ctx, nil, nil, handler)
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
