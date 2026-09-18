package auth

import (
	"errors"
	"testing"
	"time"
)

// A state must be usable exactly once. Without that, a callback URL captured
// from a browser history or a referrer could be replayed into a second session.
func TestStateStoreSingleUse(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	s := newStateStore(DefaultStateTTL, func() time.Time { return now })

	state, err := s.put(pending{nonce: "n", verifier: "v", provider: "github"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.take(state)
	if err != nil {
		t.Fatalf("first take: %v", err)
	}
	if got.nonce != "n" || got.verifier != "v" || got.provider != "github" {
		t.Fatalf("take returned %+v", got)
	}
	if _, err := s.take(state); !errors.Is(err, errUnknownState) {
		t.Fatalf("second take: err = %v, want errUnknownState", err)
	}
}

func TestStateStoreRejections(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	now := base
	s := newStateStore(10*time.Minute, func() time.Time { return now })

	live, err := s.put(pending{nonce: "n"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		state string
		at    time.Time
		ok    bool
	}{
		{"empty state", "", base, false},
		{"never issued", "Zm9vYmFy", base, false},
		{"issued, within ttl", live, base.Add(9 * time.Minute), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now = tc.at
			_, err := s.take(tc.state)
			if tc.ok != (err == nil) {
				t.Fatalf("take err = %v, want ok = %v", err, tc.ok)
			}
		})
	}

	// And the same value is dead once its TTL has passed.
	now = base
	expiring, err := s.put(pending{nonce: "n"})
	if err != nil {
		t.Fatal(err)
	}
	now = base.Add(10 * time.Minute)
	if _, err := s.take(expiring); !errors.Is(err, errUnknownState) {
		t.Fatalf("exactly at the TTL: err = %v, want errUnknownState", err)
	}
}

func TestStateValuesAreUniqueAndOpaque(t *testing.T) {
	now := time.Now()
	s := newStateStore(DefaultStateTTL, func() time.Time { return now })
	seen := make(map[string]bool)
	for range 1000 {
		v, err := s.put(pending{})
		if err != nil {
			t.Fatal(err)
		}
		if seen[v] {
			t.Fatalf("duplicate state %q", v)
		}
		if len(v) < 40 {
			t.Fatalf("state %q is only %d characters; too short to be unguessable", v, len(v))
		}
		seen[v] = true
	}
}

// /auth is unauthenticated, so the state map must not be a way to exhaust
// memory.
func TestStateStoreIsBounded(t *testing.T) {
	base := time.Now()
	now := base
	s := newStateStore(time.Hour, func() time.Time { return now })
	for range maxPendingStates + 500 {
		now = now.Add(time.Millisecond)
		if _, err := s.put(pending{}); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	n := len(s.entries)
	s.mu.Unlock()
	if n > maxPendingStates {
		t.Fatalf("state store holds %d entries, cap is %d", n, maxPendingStates)
	}
}

func TestStateStorePrunesExpired(t *testing.T) {
	base := time.Now()
	now := base
	s := newStateStore(time.Minute, func() time.Time { return now })
	for range 100 {
		if _, err := s.put(pending{}); err != nil {
			t.Fatal(err)
		}
	}
	now = base.Add(2 * time.Minute)
	if _, err := s.put(pending{}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	n := len(s.entries)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("state store holds %d entries after everything expired, want 1", n)
	}
}
