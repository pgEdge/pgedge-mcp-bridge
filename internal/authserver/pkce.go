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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// ValidatePKCE validates that the code_verifier matches the stored code_challenge.
// Only S256 method is supported per security best practices.
func ValidatePKCE(codeVerifier, codeChallenge, codeChallengeMethod string) bool {
	if codeChallengeMethod != "S256" {
		// Only S256 is supported for security reasons
		return false
	}

	if codeVerifier == "" || codeChallenge == "" {
		return false
	}

	// Compute S256 challenge from verifier
	// challenge = BASE64URL(SHA256(verifier))
	h := sha256.Sum256([]byte(codeVerifier))
	computedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(computedChallenge), []byte(codeChallenge)) == 1
}

// ValidateCodeVerifier checks if the code verifier meets RFC 7636 requirements.
// The verifier must be between 43 and 128 characters and contain only
// unreserved characters [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~"
func ValidateCodeVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}

	for _, c := range verifier {
		if !isUnreservedChar(c) {
			return false
		}
	}

	return true
}

// ValidateCodeChallenge checks if the code challenge meets RFC 7636 requirements.
// For S256, the challenge is a base64url-encoded SHA256 hash (43 characters).
func ValidateCodeChallenge(challenge string) bool {
	// S256 produces a 32-byte hash, which base64url encodes to 43 characters
	if len(challenge) != 43 {
		return false
	}

	for _, c := range challenge {
		if !isBase64URLChar(c) {
			return false
		}
	}

	return true
}

// ValidateCodeChallengeMethod checks if the method is supported.
// Only S256 is supported; plain is rejected for security.
func ValidateCodeChallengeMethod(method string) bool {
	return method == "S256"
}

// isUnreservedChar checks if a character is an unreserved character per RFC 7636.
func isUnreservedChar(c rune) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

// isBase64URLChar checks if a character is valid in base64url encoding.
func isBase64URLChar(c rune) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_'
}
