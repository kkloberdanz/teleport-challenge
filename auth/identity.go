// Package auth provides TLS-based identity extraction and authorization helpers.
package auth

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// identityKey is used in context.WithValue.
// It is suggested that this should be the concrete type struct{}
//
// From the docs:
// > context keys often have concrete type struct{}
// See: https://pkg.go.dev/context#WithValue
type identityKey struct{}

// Identity represents the authenticated caller, extracted from a client TLS certificate.
type Identity struct {
	Username string // CN from the certificate subject
	Role     string // First OU from the certificate subject ("admin" or "client")
}

// NewContext returns a new context with the given identity attached.
// Only stores one identity per context.
func NewContext(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// FromContext retrieves the identity previously stored by the interceptor.
// Returns a PermissionDenied error if no identity is present.
func FromContext(ctx context.Context) (Identity, error) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	if !ok {
		return Identity{}, status.Error(codes.PermissionDenied, "no identity in context")
	}
	return id, nil
}

// IsAdmin returns true if the identity has the admin role.
func (id Identity) IsAdmin() bool {
	return id.Role == "admin"
}

// identityFromTLS extracts the caller's identity from the gRPC peer TLS certificate.
func identityFromTLS(ctx context.Context) (Identity, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return Identity{}, status.Error(codes.PermissionDenied, "no peer info in context")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return Identity{}, status.Error(codes.PermissionDenied, "peer is not using TLS")
	}

	if len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return Identity{}, status.Error(codes.PermissionDenied, "no verified certificate chain")
	}

	cert := tlsInfo.State.VerifiedChains[0][0]
	if len(cert.Subject.OrganizationalUnit) == 0 {
		return Identity{}, status.Error(codes.PermissionDenied, "certificate has no organizational unit")
	}
	role := cert.Subject.OrganizationalUnit[0]
	if role != "admin" && role != "client" {
		return Identity{}, status.Errorf(codes.PermissionDenied, "unrecognized role %q", role)
	}

	return Identity{
		Username: cert.Subject.CommonName,
		Role:     role,
	}, nil
}
