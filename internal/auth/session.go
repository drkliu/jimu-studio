package auth

import (
	"context"
	"time"
)

// Session returns a presentation-safe view and refreshes server-side token state when needed.
func (broker *Broker) Session(ctx context.Context, sessionID string) (SessionView, bool) {
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return SessionView{}, false
	}

	session.mu.Lock()
	now := broker.now()
	if !session.active || !now.Before(session.expires) {
		session.mu.Unlock()
		broker.invalidate(sessionID, session)
		return SessionView{}, false
	}
	if !session.tokenExpiry.IsZero() && !now.Add(broker.refreshSkew).Before(session.tokenExpiry) {
		if session.refreshToken == "" {
			session.mu.Unlock()
			broker.invalidate(sessionID, session)
			return SessionView{}, false
		}
		tenant := broker.tenants[session.tenantID]
		token, err := tenant.Identity.Refresh(ctx, session.refreshToken)
		if err != nil || token.AccessToken == "" {
			session.mu.Unlock()
			broker.invalidate(sessionID, session)
			return SessionView{}, false
		}
		newClient, err := broker.clientFactory(context.WithoutCancel(ctx), tenant.ProviderBaseURL, token.AccessToken)
		if err != nil {
			session.mu.Unlock()
			broker.invalidate(sessionID, session)
			return SessionView{}, false
		}
		oldClient := session.client
		session.client = newClient
		session.accessToken = token.AccessToken
		if token.RefreshToken != "" {
			session.refreshToken = token.RefreshToken
		}
		session.tokenExpiry = token.Expiry
		oldClient.Close()
	}
	tenant := broker.tenants[session.tenantID]
	view := SessionView{
		TenantID:    session.tenantID,
		TenantName:  tenant.Name,
		Subject:     session.subject,
		DisplayName: session.displayName,
		Roles:       append([]string(nil), session.roles...),
		CSRF:        session.csrf,
	}
	session.mu.Unlock()
	return view, true
}

// Switch invalidates all old tenant state before starting fresh target authorization.
func (broker *Broker) Switch(sessionID, csrf, tenantID string) (Pending, error) {
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return Pending{}, ErrUnknownSession
	}
	session.mu.Lock()
	if !session.active {
		session.mu.Unlock()
		return Pending{}, ErrUnknownSession
	}
	if !constantTimeEqual(session.csrf, csrf) {
		session.mu.Unlock()
		return Pending{}, ErrInvalidCSRF
	}
	session.mu.Unlock()
	broker.invalidate(sessionID, session)
	return broker.Begin(tenantID)
}

// Logout validates CSRF and disposes all server-side tenant state.
func (broker *Broker) Logout(sessionID, csrf string) error {
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return ErrUnknownSession
	}
	session.mu.Lock()
	valid := session.active && constantTimeEqual(session.csrf, csrf)
	session.mu.Unlock()
	if !valid {
		return ErrInvalidCSRF
	}
	broker.invalidate(sessionID, session)
	return nil
}

func (broker *Broker) invalidate(sessionID string, session *sessionState) {
	broker.mu.Lock()
	if broker.sessions[sessionID] == session {
		delete(broker.sessions, sessionID)
	}
	broker.mu.Unlock()
	deactivate(session)
}

func deactivate(session *sessionState) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.active {
		return
	}
	session.active = false
	session.client.Close()
	session.accessToken = ""
	session.refreshToken = ""
	clear(session.cache)
	clear(session.optimistic)
	session.cache = nil
	session.optimistic = nil
}

func (broker *Broker) cleanupPendingLocked(now time.Time) {
	for id, pending := range broker.pending {
		if !now.Before(pending.expires) {
			delete(broker.pending, id)
		}
	}
}

func (broker *Broker) evictOldestPendingLocked() {
	for len(broker.pending) >= broker.maxPending {
		var oldestID string
		var oldest time.Time
		for id, pending := range broker.pending {
			if oldestID == "" || pending.created.Before(oldest) {
				oldestID, oldest = id, pending.created
			}
		}
		delete(broker.pending, oldestID)
	}
}

func (broker *Broker) cleanupSessionsLocked(now time.Time) []*sessionState {
	var removed []*sessionState
	for id, session := range broker.sessions {
		if !now.Before(session.expires) {
			delete(broker.sessions, id)
			removed = append(removed, session)
		}
	}
	return removed
}

func (broker *Broker) evictOldestSessionLocked() *sessionState {
	if len(broker.sessions) < broker.maxSessions {
		return nil
	}
	var oldestID string
	var oldest time.Time
	var evicted *sessionState
	for id, session := range broker.sessions {
		if oldestID == "" || session.created.Before(oldest) {
			oldestID, oldest, evicted = id, session.created, session
		}
	}
	delete(broker.sessions, oldestID)
	return evicted
}
