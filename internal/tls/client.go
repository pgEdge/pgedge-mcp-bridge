package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// NewClientTLSConfig creates a tls.Config suitable for client use based on
// the provided configuration. It supports custom CA certificates, client
// certificates for mutual TLS, and various TLS options.
func NewClientTLSConfig(cfg *config.TLSClientConfig) (*tls.Config, error) {
	if cfg == nil {
		// Return a default TLS config if no configuration is provided
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
		}, nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load CA certificate if specified
	if cfg.CACert != "" {
		caPool, err := LoadCACertPool(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("loading CA certificate: %w", err)
		}
		tlsConfig.RootCAs = caPool
	} else {
		// Use system certificate pool if no CA is specified
		systemPool, err := x509.SystemCertPool()
		if err != nil {
			// On some systems (e.g., Windows), SystemCertPool may fail
			// In that case, use an empty pool and rely on InsecureSkipVerify
			// or the default verification
			systemPool = x509.NewCertPool()
		}
		tlsConfig.RootCAs = systemPool
	}

	// Load client certificate pair if specified (for mutual TLS)
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := LoadCertificate(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	} else if cfg.CertFile != "" || cfg.KeyFile != "" {
		return nil, fmt.Errorf("both cert_file and key_file must be specified for client certificates")
	}

	// Configure InsecureSkipVerify (use with caution)
	tlsConfig.InsecureSkipVerify = cfg.InsecureSkipVerify

	// Configure ServerName for SNI
	if cfg.ServerName != "" {
		tlsConfig.ServerName = cfg.ServerName
	}

	return tlsConfig, nil
}
