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
	"crypto/subtle"
	"errors"
	"os"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
)

// UserAuthenticator handles user authentication for the built-in mode.
type UserAuthenticator interface {
	// Authenticate validates credentials and returns user info.
	Authenticate(username, password string) (*UserInfo, error)

	// GetUser retrieves user info by ID.
	GetUser(userID string) (*UserInfo, error)
}

// UserInfo represents authenticated user information.
type UserInfo struct {
	// ID is the unique user identifier (same as username in built-in mode).
	ID string

	// Username is the user's login name.
	Username string

	// Scopes are the scopes this user is allowed to request.
	Scopes []string
}

// BuiltInAuthenticator authenticates users against a configured user list.
type BuiltInAuthenticator struct {
	users map[string]*builtInUser
	mu    sync.RWMutex
}

type builtInUser struct {
	username     string
	passwordHash []byte
	scopes       []string
}

// NewBuiltInAuthenticator creates a new authenticator from configuration.
func NewBuiltInAuthenticator(cfg *config.BuiltInAuthConfig) (*BuiltInAuthenticator, error) {
	ba := &BuiltInAuthenticator{
		users: make(map[string]*builtInUser),
	}

	for _, userCfg := range cfg.Users {
		var passwordHash []byte
		var err error

		if userCfg.PasswordHash != "" {
			// Use pre-hashed password
			passwordHash = []byte(userCfg.PasswordHash)
		} else if userCfg.PasswordEnv != "" {
			// Hash password from environment variable
			plaintext := os.Getenv(userCfg.PasswordEnv)
			if plaintext == "" {
				return nil, errors.New("password environment variable is empty: " + userCfg.PasswordEnv)
			}
			passwordHash, err = bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
			if err != nil {
				return nil, errors.New("failed to hash password for user: " + userCfg.Username)
			}
		} else {
			return nil, errors.New("no password configured for user: " + userCfg.Username)
		}

		ba.users[userCfg.Username] = &builtInUser{
			username:     userCfg.Username,
			passwordHash: passwordHash,
			scopes:       userCfg.Scopes,
		}
	}

	return ba, nil
}

// Authenticate validates the username and password.
func (ba *BuiltInAuthenticator) Authenticate(username, password string) (*UserInfo, error) {
	ba.mu.RLock()
	user, ok := ba.users[username]
	ba.mu.RUnlock()

	if !ok {
		// Perform a dummy bcrypt comparison to prevent timing attacks
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$10$dummy.hash.for.timing.attack.prevention"),
			[]byte(password),
		)
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(user.passwordHash, []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &UserInfo{
		ID:       user.username,
		Username: user.username,
		Scopes:   user.scopes,
	}, nil
}

// GetUser retrieves user info by ID.
func (ba *BuiltInAuthenticator) GetUser(userID string) (*UserInfo, error) {
	ba.mu.RLock()
	user, ok := ba.users[userID]
	ba.mu.RUnlock()

	if !ok {
		return nil, ErrUserNotFound
	}

	return &UserInfo{
		ID:       user.username,
		Username: user.username,
		Scopes:   user.scopes,
	}, nil
}

// FilterScopes returns the intersection of requested scopes and user's allowed scopes.
func FilterScopes(requested []string, allowed []string) []string {
	allowedSet := make(map[string]bool)
	for _, s := range allowed {
		allowedSet[s] = true
	}

	var result []string
	for _, s := range requested {
		if allowedSet[s] {
			result = append(result, s)
		}
	}
	return result
}

// ValidateClientSecret validates a client secret using constant-time comparison.
func ValidateClientSecret(provided, stored string) bool {
	if stored == "" {
		// Public client - no secret required
		return true
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(stored)) == 1
}

// Authentication errors
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserNotFound       = errors.New("user not found")
)

// Ensure BuiltInAuthenticator implements UserAuthenticator
var _ UserAuthenticator = (*BuiltInAuthenticator)(nil)
