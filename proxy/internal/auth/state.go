package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// maxPendingStates bounds the memory an unauthenticated caller can make the
// process hold by hammering /auth. Oldest entries are evicted first.
const maxPendingStates = 4096

var errUnknownState = errors.New("auth: unknown or expired state")

// pending is everything /callback needs to finish a sign-in that /auth began.
type pending struct {
	nonce    string // OIDC nonce, echoed in the ID token
	verifier string // PKCE code_verifier
	provider string // the provider Decap asked for; echoed in the handshake
	created  time.Time
	expires  time.Time
}

// stateStore holds half-finished sign-ins. It is in memory and single use,
// which is enough for replay protection and avoids any cookie/SameSite
// question, but it binds /auth and /callback to the same process: a restart
// mid-login, or a second replica, fails the login.
type stateStore struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]pending
}

func newStateStore(ttl time.Duration, now func() time.Time) *stateStore {
	return &stateStore{ttl: ttl, now: now, entries: make(map[string]pending)}
}

// put stores p under a fresh random state value and returns that value.
func (s *stateStore) put(p pending) (string, error) {
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	now := s.now()
	p.created = now
	p.expires = now.Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	for len(s.entries) >= maxPendingStates {
		s.evictOldestLocked()
	}
	s.entries[state] = p
	return state, nil
}

// take removes and returns the entry for state. A state is single use: a
// second /callback with the same value fails.
func (s *stateStore) take(state string) (pending, error) {
	if state == "" {
		return pending{}, errUnknownState
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	p, ok := s.entries[state]
	if !ok {
		return pending{}, errUnknownState
	}
	delete(s.entries, state)
	if !now.Before(p.expires) {
		return pending{}, errUnknownState
	}
	return p, nil
}

func (s *stateStore) pruneLocked(now time.Time) {
	for k, p := range s.entries {
		if !now.Before(p.expires) {
			delete(s.entries, k)
		}
	}
}

func (s *stateStore) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for k, p := range s.entries {
		if oldestKey == "" || p.created.Before(oldest) {
			oldestKey, oldest = k, p.created
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
