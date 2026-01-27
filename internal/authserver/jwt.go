/*-------------------------------------------------------------------------
 *
 * pgEdge MCP Bridge
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package authserver

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// TokenIssuer handles JWT token creation and signing.
type TokenIssuer struct {
	issuer     string
	signer     jose.Signer
	keyID      string
	algorithm  jose.SignatureAlgorithm
	privateKey interface{}
	publicKey  interface{}
}

// NewTokenIssuer creates a new token issuer from configuration.
func NewTokenIssuer(issuer, algorithm, keyFile, keyID string, generateKey bool) (*TokenIssuer, error) {
	alg := jose.SignatureAlgorithm(algorithm)

	var privateKey interface{}
	var publicKey interface{}
	var err error

	if generateKey {
		privateKey, publicKey, err = generateKeyPair(alg)
		if err != nil {
			return nil, fmt.Errorf("generating key pair: %w", err)
		}
	} else {
		privateKey, publicKey, err = loadKeyFromFile(keyFile, alg)
		if err != nil {
			return nil, fmt.Errorf("loading key from file: %w", err)
		}
	}

	// Generate key ID if not provided
	if keyID == "" {
		keyID = generateKeyID()
	}

	signerKey := jose.SigningKey{
		Algorithm: alg,
		Key:       privateKey,
	}

	signerOpts := (&jose.SignerOptions{}).
		WithType("JWT").
		WithHeader("kid", keyID)

	signer, err := jose.NewSigner(signerKey, signerOpts)
	if err != nil {
		return nil, fmt.Errorf("creating signer: %w", err)
	}

	return &TokenIssuer{
		issuer:     issuer,
		signer:     signer,
		keyID:      keyID,
		algorithm:  alg,
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// AccessTokenClaims represents the claims in an access token.
type AccessTokenClaims struct {
	jwt.Claims
	Scope    string `json:"scope,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

// IssueAccessToken creates a new signed JWT access token.
func (ti *TokenIssuer) IssueAccessToken(
	subject string,
	audience []string,
	scope string,
	clientID string,
	lifetime time.Duration,
) (string, string, error) {
	now := time.Now()
	jti := generateJTI()

	claims := AccessTokenClaims{
		Claims: jwt.Claims{
			Issuer:    ti.issuer,
			Subject:   subject,
			Audience:  jwt.Audience(audience),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Expiry:    jwt.NewNumericDate(now.Add(lifetime)),
			ID:        jti,
		},
		Scope:    scope,
		ClientID: clientID,
	}

	token, err := jwt.Signed(ti.signer).Claims(claims).Serialize()
	if err != nil {
		return "", "", fmt.Errorf("signing access token: %w", err)
	}

	return token, jti, nil
}

// RefreshTokenValue generates a new random refresh token value.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return encodeBase64URL(b), nil
}

// GenerateAuthorizationCode generates a new random authorization code.
func GenerateAuthorizationCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return encodeBase64URL(b), nil
}

// JWKS returns the JSON Web Key Set containing the public key.
func (ti *TokenIssuer) JWKS() *jose.JSONWebKeySet {
	jwk := jose.JSONWebKey{
		Key:       ti.publicKey,
		KeyID:     ti.keyID,
		Algorithm: string(ti.algorithm),
		Use:       "sig",
	}

	return &jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{jwk},
	}
}

// Helper functions

func generateKeyPair(alg jose.SignatureAlgorithm) (privateKey, publicKey interface{}, err error) {
	switch {
	case strings.HasPrefix(string(alg), "RS"):
		// RSA key
		bits := 2048
		if alg == jose.RS384 {
			bits = 3072
		} else if alg == jose.RS512 {
			bits = 4096
		}
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return nil, nil, err
		}
		return key, &key.PublicKey, nil

	case strings.HasPrefix(string(alg), "ES"):
		// ECDSA key
		var curve elliptic.Curve
		switch alg {
		case jose.ES256:
			curve = elliptic.P256()
		case jose.ES384:
			curve = elliptic.P384()
		case jose.ES512:
			curve = elliptic.P521()
		default:
			return nil, nil, fmt.Errorf("unsupported ECDSA algorithm: %s", alg)
		}
		key, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		return key, &key.PublicKey, nil

	default:
		return nil, nil, fmt.Errorf("unsupported algorithm: %s", alg)
	}
}

func loadKeyFromFile(path string, alg jose.SignatureAlgorithm) (privateKey, publicKey interface{}, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil, fmt.Errorf("failed to decode PEM block")
	}

	var key crypto.PrivateKey

	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	default:
		return nil, nil, fmt.Errorf("unsupported key type: %s", block.Type)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("parsing private key: %w", err)
	}

	// Validate key type matches algorithm
	switch k := key.(type) {
	case *rsa.PrivateKey:
		if !strings.HasPrefix(string(alg), "RS") {
			return nil, nil, fmt.Errorf("RSA key provided but algorithm is %s", alg)
		}
		return k, &k.PublicKey, nil
	case *ecdsa.PrivateKey:
		if !strings.HasPrefix(string(alg), "ES") {
			return nil, nil, fmt.Errorf("ECDSA key provided but algorithm is %s", alg)
		}
		return k, &k.PublicKey, nil
	default:
		return nil, nil, fmt.Errorf("unsupported key type: %T", key)
	}
}

func generateKeyID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return encodeBase64URL(b)
}

func generateJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return encodeBase64URL(b)
}

func encodeBase64URL(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, (len(data)*8+5)/6)

	for i, b := 0, uint(0); i < len(result); i++ {
		if i%4 == 0 && i > 0 {
			b >>= 2
		}
		byteIndex := (i * 6) / 8
		bitOffset := (i * 6) % 8

		if byteIndex < len(data) {
			b = uint(data[byteIndex])
			if bitOffset > 2 && byteIndex+1 < len(data) {
				b |= uint(data[byteIndex+1]) << 8
			}
			result[i] = alphabet[(b>>bitOffset)&0x3F]
		}
	}

	return string(result)
}

// GenerateClientID generates a new random client ID.
func GenerateClientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return encodeBase64URL(b)
}

// GenerateClientSecret generates a new random client secret.
func GenerateClientSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return encodeBase64URL(b)
}
