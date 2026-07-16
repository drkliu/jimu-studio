package auth

import (
	"context"
	"errors"
)

const maxSessionDrafts = 128

// ValidateCSRF verifies a mutation proof against an active volatile session.
func (broker *Broker) ValidateCSRF(sessionID, csrf string) error {
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return ErrUnknownSession
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.active {
		return ErrUnknownSession
	}
	if !constantTimeEqual(session.csrf, csrf) {
		return ErrInvalidCSRF
	}
	return nil
}

// WithClient runs one operation against the current token-bound client. A
// concurrent tenant switch closes that client and cancels its in-flight work.
func (broker *Broker) WithClient(ctx context.Context, sessionID string, operation func(TenantClient) error) error {
	if operation == nil {
		return errors.New("tenant operation is required")
	}
	if _, ok := broker.Session(ctx, sessionID); !ok {
		return ErrUnknownSession
	}
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return ErrUnknownSession
	}
	session.mu.Lock()
	if !session.active || session.client == nil {
		session.mu.Unlock()
		return ErrUnknownSession
	}
	client := session.client
	session.mu.Unlock()
	return operation(client)
}

// DraftKey returns a stable, server-held idempotency key for one mutation basis.
func (broker *Broker) DraftKey(sessionID, scope string) (string, error) {
	if scope == "" || len(scope) > 1024 {
		return "", errors.New("draft scope must contain 1 to 1024 bytes")
	}
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return "", ErrUnknownSession
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.active || session.optimistic == nil {
		return "", ErrUnknownSession
	}
	if existing, ok := session.optimistic[scope].(string); ok && existing != "" {
		return existing, nil
	}
	if len(session.optimistic) >= maxSessionDrafts {
		return "", errors.New("session draft limit reached")
	}
	key, err := broker.random(24)
	if err != nil {
		return "", err
	}
	session.optimistic[scope] = key
	return key, nil
}

// ClearDraft disposes a completed mutation's idempotency state.
func (broker *Broker) ClearDraft(sessionID, scope string) {
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.active && session.optimistic != nil {
		delete(session.optimistic, scope)
	}
}
