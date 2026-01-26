/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// TestCerts holds paths to generated test certificates
type TestCerts struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ClientCert string
	ClientKey  string
}

// generateTestCertificates creates test certificates in a temporary directory
func generateTestCertificates(t *testing.T, dir string) *TestCerts {
	t.Helper()

	// Generate CA key pair
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	// Create CA certificate
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
			CommonName:   "Test CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	// Generate server key pair
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate server key: %v", err)
	}

	// Create server certificate
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Server"},
			CommonName:   "localhost",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"},
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create server certificate: %v", err)
	}

	// Generate client key pair
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate client key: %v", err)
	}

	// Create client certificate
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization: []string{"Test Client"},
			CommonName:   "test-client",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create client certificate: %v", err)
	}

	// Write CA certificate
	caFile := filepath.Join(dir, "ca.crt")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	if err := os.WriteFile(caFile, caPEM, 0644); err != nil {
		t.Fatalf("Failed to write CA certificate: %v", err)
	}

	// Write server certificate
	certFile := filepath.Join(dir, "server.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatalf("Failed to write server certificate: %v", err)
	}

	// Write server key
	keyFile := filepath.Join(dir, "server.key")
	keyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		t.Fatalf("Failed to marshal server key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyFile, keyPEM, 0644); err != nil {
		t.Fatalf("Failed to write server key: %v", err)
	}

	// Write client certificate
	clientCertFile := filepath.Join(dir, "client.crt")
	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	if err := os.WriteFile(clientCertFile, clientCertPEM, 0644); err != nil {
		t.Fatalf("Failed to write client certificate: %v", err)
	}

	// Write client key
	clientKeyFile := filepath.Join(dir, "client.key")
	clientKeyBytes, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatalf("Failed to marshal client key: %v", err)
	}
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: clientKeyBytes})
	if err := os.WriteFile(clientKeyFile, clientKeyPEM, 0644); err != nil {
		t.Fatalf("Failed to write client key: %v", err)
	}

	return &TestCerts{
		CertFile:   certFile,
		KeyFile:    keyFile,
		CAFile:     caFile,
		ClientCert: clientCertFile,
		ClientKey:  clientKeyFile,
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		expected  uint16
		expectErr bool
	}{
		{
			name:     "empty string returns zero (default)",
			version:  "",
			expected: 0,
		},
		{
			name:     "TLS 1.0",
			version:  "1.0",
			expected: tls.VersionTLS10,
		},
		{
			name:     "TLS 1.1",
			version:  "1.1",
			expected: tls.VersionTLS11,
		},
		{
			name:     "TLS 1.2",
			version:  "1.2",
			expected: tls.VersionTLS12,
		},
		{
			name:     "TLS 1.3",
			version:  "1.3",
			expected: tls.VersionTLS13,
		},
		{
			name:      "invalid version",
			version:   "1.4",
			expectErr: true,
		},
		{
			name:      "non-numeric version",
			version:   "invalid",
			expectErr: true,
		},
		{
			name:      "negative version",
			version:   "-1",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTLSVersion(tt.version)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestLoadCertificate(t *testing.T) {
	dir := t.TempDir()
	certs := generateTestCertificates(t, dir)

	t.Run("valid certificate and key", func(t *testing.T) {
		cert, err := LoadCertificate(certs.CertFile, certs.KeyFile)
		if err != nil {
			t.Fatalf("Failed to load certificate: %v", err)
		}

		if len(cert.Certificate) == 0 {
			t.Error("Expected at least one certificate in chain")
		}
	})

	t.Run("invalid cert file path", func(t *testing.T) {
		_, err := LoadCertificate("/nonexistent/cert.pem", certs.KeyFile)
		if err == nil {
			t.Error("Expected error for nonexistent cert file")
		}
	})

	t.Run("invalid key file path", func(t *testing.T) {
		_, err := LoadCertificate(certs.CertFile, "/nonexistent/key.pem")
		if err == nil {
			t.Error("Expected error for nonexistent key file")
		}
	})

	t.Run("mismatched cert and key", func(t *testing.T) {
		// Try to load server cert with client key
		_, err := LoadCertificate(certs.CertFile, certs.ClientKey)
		if err == nil {
			t.Error("Expected error for mismatched cert and key")
		}
	})

	t.Run("invalid cert content", func(t *testing.T) {
		invalidCertFile := filepath.Join(dir, "invalid.crt")
		if err := os.WriteFile(invalidCertFile, []byte("invalid cert"), 0644); err != nil {
			t.Fatalf("Failed to create invalid cert file: %v", err)
		}

		_, err := LoadCertificate(invalidCertFile, certs.KeyFile)
		if err == nil {
			t.Error("Expected error for invalid cert content")
		}
	})

	t.Run("invalid key content", func(t *testing.T) {
		invalidKeyFile := filepath.Join(dir, "invalid.key")
		if err := os.WriteFile(invalidKeyFile, []byte("invalid key"), 0644); err != nil {
			t.Fatalf("Failed to create invalid key file: %v", err)
		}

		_, err := LoadCertificate(certs.CertFile, invalidKeyFile)
		if err == nil {
			t.Error("Expected error for invalid key content")
		}
	})
}

func TestLoadCACertPool(t *testing.T) {
	dir := t.TempDir()
	certs := generateTestCertificates(t, dir)

	t.Run("valid CA certificate", func(t *testing.T) {
		pool, err := LoadCACertPool(certs.CAFile)
		if err != nil {
			t.Fatalf("Failed to load CA cert pool: %v", err)
		}

		if pool == nil {
			t.Error("Expected non-nil cert pool")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := LoadCACertPool("/nonexistent/ca.pem")
		if err == nil {
			t.Error("Expected error for nonexistent file")
		}
	})

	t.Run("invalid CA content", func(t *testing.T) {
		invalidCAFile := filepath.Join(dir, "invalid_ca.crt")
		if err := os.WriteFile(invalidCAFile, []byte("not a certificate"), 0644); err != nil {
			t.Fatalf("Failed to create invalid CA file: %v", err)
		}

		_, err := LoadCACertPool(invalidCAFile)
		if err == nil {
			t.Error("Expected error for invalid CA content")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		emptyCAFile := filepath.Join(dir, "empty_ca.crt")
		if err := os.WriteFile(emptyCAFile, []byte{}, 0644); err != nil {
			t.Fatalf("Failed to create empty CA file: %v", err)
		}

		_, err := LoadCACertPool(emptyCAFile)
		if err == nil {
			t.Error("Expected error for empty CA file")
		}
	})
}

func TestParseClientAuthType(t *testing.T) {
	tests := []struct {
		name      string
		authType  string
		expected  tls.ClientAuthType
		expectErr bool
	}{
		{
			name:     "empty string defaults to NoClientCert",
			authType: "",
			expected: tls.NoClientCert,
		},
		{
			name:     "none",
			authType: "none",
			expected: tls.NoClientCert,
		},
		{
			name:     "request",
			authType: "request",
			expected: tls.RequestClientCert,
		},
		{
			name:     "require",
			authType: "require",
			expected: tls.RequireAnyClientCert,
		},
		{
			name:     "verify",
			authType: "verify",
			expected: tls.VerifyClientCertIfGiven,
		},
		{
			name:     "require_and_verify",
			authType: "require_and_verify",
			expected: tls.RequireAndVerifyClientCert,
		},
		{
			name:      "invalid type",
			authType:  "invalid",
			expectErr: true,
		},
		{
			name:      "typo in type",
			authType:  "reqire",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseClientAuthType(tt.authType)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNewServerTLSConfig(t *testing.T) {
	dir := t.TempDir()
	certs := generateTestCertificates(t, dir)

	t.Run("nil config returns error", func(t *testing.T) {
		_, err := NewServerTLSConfig(nil)
		if err == nil {
			t.Error("Expected error for nil config")
		}
	})

	t.Run("disabled TLS returns error", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled: false,
		}
		_, err := NewServerTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for disabled TLS")
		}
	})

	t.Run("valid minimal config", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:  true,
			CertFile: certs.CertFile,
			KeyFile:  certs.KeyFile,
		}

		tlsCfg, err := NewServerTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create server TLS config: %v", err)
		}

		if len(tlsCfg.Certificates) != 1 {
			t.Errorf("Expected 1 certificate, got %d", len(tlsCfg.Certificates))
		}

		if tlsCfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("Expected default min version TLS 1.2, got %d", tlsCfg.MinVersion)
		}
	})

	t.Run("full config with client CA", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:    true,
			CertFile:   certs.CertFile,
			KeyFile:    certs.KeyFile,
			ClientCA:   certs.CAFile,
			ClientAuth: "require_and_verify",
			MinVersion: "1.2",
			MaxVersion: "1.3",
		}

		tlsCfg, err := NewServerTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create server TLS config: %v", err)
		}

		if tlsCfg.ClientCAs == nil {
			t.Error("Expected ClientCAs to be set")
		}

		if tlsCfg.ClientAuth != tls.RequireAndVerifyClientCert {
			t.Errorf("Expected RequireAndVerifyClientCert, got %v", tlsCfg.ClientAuth)
		}

		if tlsCfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("Expected min version TLS 1.2, got %d", tlsCfg.MinVersion)
		}

		if tlsCfg.MaxVersion != tls.VersionTLS13 {
			t.Errorf("Expected max version TLS 1.3, got %d", tlsCfg.MaxVersion)
		}
	})

	t.Run("invalid cert file", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  certs.KeyFile,
		}

		_, err := NewServerTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid cert file")
		}
	})

	t.Run("invalid client CA", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:  true,
			CertFile: certs.CertFile,
			KeyFile:  certs.KeyFile,
			ClientCA: "/nonexistent/ca.pem",
		}

		_, err := NewServerTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid client CA")
		}
	})

	t.Run("invalid client auth type", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:    true,
			CertFile:   certs.CertFile,
			KeyFile:    certs.KeyFile,
			ClientAuth: "invalid",
		}

		_, err := NewServerTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid client auth type")
		}
	})

	t.Run("invalid min version", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:    true,
			CertFile:   certs.CertFile,
			KeyFile:    certs.KeyFile,
			MinVersion: "invalid",
		}

		_, err := NewServerTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid min version")
		}
	})

	t.Run("invalid max version", func(t *testing.T) {
		cfg := &config.TLSConfig{
			Enabled:    true,
			CertFile:   certs.CertFile,
			KeyFile:    certs.KeyFile,
			MaxVersion: "invalid",
		}

		_, err := NewServerTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid max version")
		}
	})
}

func TestNewClientTLSConfig(t *testing.T) {
	dir := t.TempDir()
	certs := generateTestCertificates(t, dir)

	t.Run("nil config returns default", func(t *testing.T) {
		tlsCfg, err := NewClientTLSConfig(nil)
		if err != nil {
			t.Fatalf("Unexpected error for nil config: %v", err)
		}

		if tlsCfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("Expected default min version TLS 1.2, got %d", tlsCfg.MinVersion)
		}
	})

	t.Run("empty config uses system cert pool", func(t *testing.T) {
		cfg := &config.TLSClientConfig{}

		tlsCfg, err := NewClientTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client TLS config: %v", err)
		}

		if tlsCfg.RootCAs == nil {
			t.Error("Expected RootCAs to be set")
		}
	})

	t.Run("with custom CA cert", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			CACert: certs.CAFile,
		}

		tlsCfg, err := NewClientTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client TLS config: %v", err)
		}

		if tlsCfg.RootCAs == nil {
			t.Error("Expected RootCAs to be set")
		}
	})

	t.Run("with client certificate for mTLS", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			CACert:   certs.CAFile,
			CertFile: certs.ClientCert,
			KeyFile:  certs.ClientKey,
		}

		tlsCfg, err := NewClientTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client TLS config: %v", err)
		}

		if len(tlsCfg.Certificates) != 1 {
			t.Errorf("Expected 1 client certificate, got %d", len(tlsCfg.Certificates))
		}
	})

	t.Run("with insecure skip verify", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			InsecureSkipVerify: true,
		}

		tlsCfg, err := NewClientTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client TLS config: %v", err)
		}

		if !tlsCfg.InsecureSkipVerify {
			t.Error("Expected InsecureSkipVerify to be true")
		}
	})

	t.Run("with server name", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			ServerName: "example.com",
		}

		tlsCfg, err := NewClientTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client TLS config: %v", err)
		}

		if tlsCfg.ServerName != "example.com" {
			t.Errorf("Expected ServerName 'example.com', got '%s'", tlsCfg.ServerName)
		}
	})

	t.Run("invalid CA cert", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			CACert: "/nonexistent/ca.pem",
		}

		_, err := NewClientTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid CA cert")
		}
	})

	t.Run("invalid client cert", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  certs.ClientKey,
		}

		_, err := NewClientTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for invalid client cert")
		}
	})

	t.Run("cert without key", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			CertFile: certs.ClientCert,
			// KeyFile intentionally omitted
		}

		_, err := NewClientTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for cert without key")
		}
	})

	t.Run("key without cert", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			// CertFile intentionally omitted
			KeyFile: certs.ClientKey,
		}

		_, err := NewClientTLSConfig(cfg)
		if err == nil {
			t.Error("Expected error for key without cert")
		}
	})

	t.Run("full config", func(t *testing.T) {
		cfg := &config.TLSClientConfig{
			CACert:             certs.CAFile,
			CertFile:           certs.ClientCert,
			KeyFile:            certs.ClientKey,
			InsecureSkipVerify: false,
			ServerName:         "test-server.local",
		}

		tlsCfg, err := NewClientTLSConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client TLS config: %v", err)
		}

		if tlsCfg.RootCAs == nil {
			t.Error("Expected RootCAs to be set")
		}

		if len(tlsCfg.Certificates) != 1 {
			t.Errorf("Expected 1 client certificate, got %d", len(tlsCfg.Certificates))
		}

		if tlsCfg.ServerName != "test-server.local" {
			t.Errorf("Expected ServerName 'test-server.local', got '%s'", tlsCfg.ServerName)
		}

		if tlsCfg.InsecureSkipVerify {
			t.Error("Expected InsecureSkipVerify to be false")
		}

		if tlsCfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("Expected min version TLS 1.2, got %d", tlsCfg.MinVersion)
		}
	})
}
