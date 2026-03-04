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
	"context"
	"errors"
	"sync"
	"time"
)

// Storage limits to prevent memory exhaustion.
const (
	maxAuthCodes     = 10000
	maxRefreshTokens = 50000
	maxClients       = 1000
)

// ErrStorageFull is returned when a storage limit is exceeded.
var ErrStorageFull = errors.New("storage limit exceeded")

// MemoryStorage is an in-memory implementation of Storage.
// Suitable for single-instance deployments and development.
type MemoryStorage struct {
	authCodes     map[string]*AuthorizationCode
	authCodesMu   sync.RWMutex
	refreshTokens map[string]*RefreshToken
	refreshMu     sync.RWMutex
	clients       map[string]*Client
	clientsMu     sync.RWMutex

	cleanupDone   chan struct{}
	cleanupCancel context.CancelFunc
}

// NewMemoryStorage creates a new in-memory storage with automatic cleanup.
func NewMemoryStorage(cleanupInterval time.Duration) *MemoryStorage {
	ctx, cancel := context.WithCancel(context.Background())
	ms := &MemoryStorage{
		authCodes:     make(map[string]*AuthorizationCode),
		refreshTokens: make(map[string]*RefreshToken),
		clients:       make(map[string]*Client),
		cleanupDone:   make(chan struct{}),
		cleanupCancel: cancel,
	}

	// Start cleanup goroutine
	go ms.cleanupLoop(ctx, cleanupInterval)

	return ms
}

// cleanupLoop periodically removes expired tokens.
func (ms *MemoryStorage) cleanupLoop(ctx context.Context, interval time.Duration) {
	defer close(ms.cleanupDone)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ms.cleanupExpired()
		}
	}
}

// cleanupExpired removes all expired authorization codes and refresh tokens.
func (ms *MemoryStorage) cleanupExpired() {
	now := time.Now()

	// Cleanup auth codes
	ms.authCodesMu.Lock()
	for code, ac := range ms.authCodes {
		if now.After(ac.ExpiresAt) {
			delete(ms.authCodes, code)
		}
	}
	ms.authCodesMu.Unlock()

	// Cleanup refresh tokens
	ms.refreshMu.Lock()
	for token, rt := range ms.refreshTokens {
		if now.After(rt.ExpiresAt) {
			delete(ms.refreshTokens, token)
		}
	}
	ms.refreshMu.Unlock()
}

// Close stops the cleanup goroutine and releases resources.
func (ms *MemoryStorage) Close() error {
	ms.cleanupCancel()
	<-ms.cleanupDone
	return nil
}

// Authorization Code methods

// StoreAuthorizationCode stores a new authorization code.
func (ms *MemoryStorage) StoreAuthorizationCode(ctx context.Context, code *AuthorizationCode) error {
	ms.authCodesMu.Lock()
	defer ms.authCodesMu.Unlock()
	if len(ms.authCodes) >= maxAuthCodes {
		return ErrStorageFull
	}
	ms.authCodes[code.Code] = code
	return nil
}

// GetAuthorizationCode retrieves an authorization code.
func (ms *MemoryStorage) GetAuthorizationCode(ctx context.Context, code string) (*AuthorizationCode, error) {
	ms.authCodesMu.RLock()
	defer ms.authCodesMu.RUnlock()

	ac, ok := ms.authCodes[code]
	if !ok {
		return nil, nil
	}

	// Check expiration
	if ac.IsExpired() {
		return nil, nil
	}

	return ac, nil
}

// DeleteAuthorizationCode removes an authorization code.
func (ms *MemoryStorage) DeleteAuthorizationCode(ctx context.Context, code string) error {
	ms.authCodesMu.Lock()
	defer ms.authCodesMu.Unlock()
	delete(ms.authCodes, code)
	return nil
}

// Refresh Token methods

// StoreRefreshToken stores a new refresh token.
func (ms *MemoryStorage) StoreRefreshToken(ctx context.Context, token *RefreshToken) error {
	ms.refreshMu.Lock()
	defer ms.refreshMu.Unlock()
	if len(ms.refreshTokens) >= maxRefreshTokens {
		return ErrStorageFull
	}
	ms.refreshTokens[token.Token] = token
	return nil
}

// GetRefreshToken retrieves a refresh token.
func (ms *MemoryStorage) GetRefreshToken(ctx context.Context, token string) (*RefreshToken, error) {
	ms.refreshMu.RLock()
	defer ms.refreshMu.RUnlock()

	rt, ok := ms.refreshTokens[token]
	if !ok {
		return nil, nil
	}

	// Check expiration
	if rt.IsExpired() {
		return nil, nil
	}

	return rt, nil
}

// DeleteRefreshToken removes a refresh token.
func (ms *MemoryStorage) DeleteRefreshToken(ctx context.Context, token string) error {
	ms.refreshMu.Lock()
	defer ms.refreshMu.Unlock()
	delete(ms.refreshTokens, token)
	return nil
}

// DeleteRefreshTokensForUser removes all refresh tokens for a user.
func (ms *MemoryStorage) DeleteRefreshTokensForUser(ctx context.Context, userID string) error {
	ms.refreshMu.Lock()
	defer ms.refreshMu.Unlock()

	for token, rt := range ms.refreshTokens {
		if rt.UserID == userID {
			delete(ms.refreshTokens, token)
		}
	}
	return nil
}

// Client methods

// StoreClient stores a new OAuth client.
func (ms *MemoryStorage) StoreClient(ctx context.Context, client *Client) error {
	ms.clientsMu.Lock()
	defer ms.clientsMu.Unlock()
	if len(ms.clients) >= maxClients {
		return ErrStorageFull
	}
	ms.clients[client.ClientID] = client
	return nil
}

// GetClient retrieves a client by ID.
func (ms *MemoryStorage) GetClient(ctx context.Context, clientID string) (*Client, error) {
	ms.clientsMu.RLock()
	defer ms.clientsMu.RUnlock()
	client, ok := ms.clients[clientID]
	if !ok {
		return nil, nil
	}
	return client, nil
}

// DeleteClient removes a client registration.
func (ms *MemoryStorage) DeleteClient(ctx context.Context, clientID string) error {
	ms.clientsMu.Lock()
	defer ms.clientsMu.Unlock()
	delete(ms.clients, clientID)
	return nil
}

// Ensure MemoryStorage implements Storage interface.
var _ Storage = (*MemoryStorage)(nil)
