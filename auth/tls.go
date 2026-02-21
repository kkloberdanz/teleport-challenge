package auth

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

// ServerTLSConfig builds a TLS config for the server that requires and verifies
// client certificates signed by the given CA.
func ServerTLSConfig(caCertPEM []byte, cert tls.Certificate) (*tls.Config, error) {
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// ClientTLSConfig builds a TLS config for a client that verifies the server
// certificate against the given CA and presents a client certificate.
func ClientTLSConfig(caCertPEM []byte, cert tls.Certificate, serverName string) (*tls.Config, error) {
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
