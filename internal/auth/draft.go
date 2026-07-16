package auth

import "errors"

const maxDraftCategoryBytes = 64

// StoreDraft keeps a typed operation snapshot in volatile session memory and
// returns an opaque browser-safe reference. Tenant teardown destroys it.
func (broker *Broker) StoreDraft(sessionID, category string, value any) (string, error) {
	if !safeDraftCategory(category) || value == nil {
		return "", errors.New("safe draft category and value are required")
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
	if len(session.optimistic) >= maxSessionDrafts {
		return "", errors.New("session draft limit reached")
	}
	for attempt := 0; attempt < 3; attempt++ {
		reference, err := broker.random(24)
		if err != nil {
			return "", err
		}
		key := draftStateKey(category, reference)
		if _, exists := session.optimistic[key]; exists {
			continue
		}
		session.optimistic[key] = value
		return reference, nil
	}
	return "", errors.New("could not allocate unique draft reference")
}

// LoadDraft returns one same-session operation snapshot.
func (broker *Broker) LoadDraft(sessionID, category, reference string) (any, bool) {
	if !safeDraftCategory(category) || reference == "" || len(reference) > 256 {
		return nil, false
	}
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return nil, false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.active || session.optimistic == nil {
		return nil, false
	}
	value, ok := session.optimistic[draftStateKey(category, reference)]
	return value, ok
}

// DeleteDraft destroys a completed or invalid operation snapshot.
func (broker *Broker) DeleteDraft(sessionID, category, reference string) {
	if !safeDraftCategory(category) || reference == "" {
		return
	}
	broker.mu.RLock()
	session := broker.sessions[sessionID]
	broker.mu.RUnlock()
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.active && session.optimistic != nil {
		delete(session.optimistic, draftStateKey(category, reference))
	}
}

func draftStateKey(category, reference string) string {
	return "state:" + category + ":" + reference
}

func safeDraftCategory(category string) bool {
	if category == "" || len(category) > maxDraftCategoryBytes {
		return false
	}
	for _, character := range category {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}
