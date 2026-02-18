// Package auth provides TLS-based identity extraction and authorization helpers.
package auth

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Identity represents the authenticated caller, extracted from a client TLS certificate.
type Identity struct {
	Username string // CN from the certificate subject
	Role     string // First OU from the certificate subject ("admin" or "client")
}

// IsAdmin returns true if the identity has the admin role.
func (id Identity) IsAdmin() bool {
	return id.Role == "admin"
}

// IdentityFromContext extracts the caller's identity from the gRPC peer TLS certificate.
func IdentityFromContext(ctx context.Context) (Identity, error) {
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
	var role string
	if len(cert.Subject.OrganizationalUnit) > 0 {
		role = cert.Subject.OrganizationalUnit[0]
	}

	return Identity{
		Username: cert.Subject.CommonName,
		Role:     role,
	}, nil
}
