/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package tls provides TLS configuration utilities for both server and client modes.
package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ParseTLSVersion converts a version string to a TLS version constant.
// Supported values: "1.2", "1.3"
// Returns 0 if the version string is empty (use default).
func ParseTLSVersion(version string) (uint16, error) {
	switch version {
	case "":
		return 0, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version: %s (supported: 1.2, 1.3)", version)
	}
}

// LoadCertificate loads a certificate and private key from files.
func LoadCertificate(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("loading certificate: %w", err)
	}
	return cert, nil
}

// LoadCACertPool loads CA certificates from a file and returns a certificate pool.
// If the file contains multiple certificates, all are added to the pool.
func LoadCACertPool(caFile string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate from %s", caFile)
	}

	return pool, nil
}

// ParseClientAuthType converts a client auth string to a tls.ClientAuthType.
// Supported values: "none", "request", "require", "verify", "require_and_verify"
// Returns NoClientCert if the string is empty.
func ParseClientAuthType(authType string) (tls.ClientAuthType, error) {
	switch authType {
	case "", "none":
		return tls.NoClientCert, nil
	case "request":
		return tls.RequestClientCert, nil
	case "require":
		return tls.RequireAnyClientCert, nil
	case "verify":
		return tls.VerifyClientCertIfGiven, nil
	case "require_and_verify":
		return tls.RequireAndVerifyClientCert, nil
	default:
		return tls.NoClientCert, fmt.Errorf("unsupported client auth type: %s (supported: none, request, require, verify, require_and_verify)", authType)
	}
}
