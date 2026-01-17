package tls

import (
	"crypto/tls"
	"fmt"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// NewServerTLSConfig creates a tls.Config suitable for server use based on
// the provided configuration. It loads the server certificate and key,
// configures client authentication if specified, and sets TLS version constraints.
func NewServerTLSConfig(cfg *config.TLSConfig) (*tls.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("TLS config is nil")
	}

	if !cfg.Enabled {
		return nil, fmt.Errorf("TLS is not enabled in config")
	}

	// Load server certificate and key
	cert, err := LoadCertificate(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("loading server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Configure client CA if specified
	if cfg.ClientCA != "" {
		clientCAPool, err := LoadCACertPool(cfg.ClientCA)
		if err != nil {
			return nil, fmt.Errorf("loading client CA: %w", err)
		}
		tlsConfig.ClientCAs = clientCAPool
	}

	// Configure client authentication mode
	clientAuth, err := ParseClientAuthType(cfg.ClientAuth)
	if err != nil {
		return nil, fmt.Errorf("parsing client auth type: %w", err)
	}
	tlsConfig.ClientAuth = clientAuth

	// Configure minimum TLS version
	if cfg.MinVersion != "" {
		minVersion, err := ParseTLSVersion(cfg.MinVersion)
		if err != nil {
			return nil, fmt.Errorf("parsing min TLS version: %w", err)
		}
		tlsConfig.MinVersion = minVersion
	} else {
		// Default to TLS 1.2 for security
		tlsConfig.MinVersion = tls.VersionTLS12
	}

	// Configure maximum TLS version
	if cfg.MaxVersion != "" {
		maxVersion, err := ParseTLSVersion(cfg.MaxVersion)
		if err != nil {
			return nil, fmt.Errorf("parsing max TLS version: %w", err)
		}
		tlsConfig.MaxVersion = maxVersion
	}

	return tlsConfig, nil
}
