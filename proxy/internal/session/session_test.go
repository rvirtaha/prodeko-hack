package session

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T, now func() time.Time, ttl time.Duration) *Store {
	t.Helper()
	s, err := NewStore(Options{
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		TTL:    ttl,
		Now:    now,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func sampleIdentity() Identity {
	return Identity{
		Subject:  "f7e1-4c2a",
		Name:     "Aino Esimerkki",
		Email:    "aino@prodeko.org",
		Username: "aino",
		Roles:    []string{"membership", "cms-editor"},
	}
}

func TestNewStoreRejectsWeakConfig(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"no secret", Options{}},
		{"short secret", Options{Secret: []byte("short")}},
		{"one byte under the minimum", Options{Secret: make([]byte, MinSecretLen-1)}},
		{"negative ttl", Options{Secret: make([]byte, MinSecretLen), TTL: -time.Second}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewStore(tc.opts); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
	if _, err := NewStore(Options{Secret: make([]byte, MinSecretLen)}); err != nil {
		t.Fatalf("exactly MinSecretLen bytes should be accepted: %v", err)
	}
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	s := testStore(t, func() time.Time { return now }, 0)

	token, expires, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if want := now.Add(DefaultTTL); !expires.Equal(want) {
		t.Errorf("expires = %v, want %v", expires, want)
	}
	if !strings.HasPrefix(token, tokenPrefix) {
		t.Errorf("token %q lacks the format prefix", token)
	}

	got, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := sampleIdentity()
	if got.Subject != want.Subject || got.Name != want.Name || got.Email != want.Email ||
		got.Username != want.Username || len(got.Roles) != len(want.Roles) {
		t.Errorf("round trip lost data: %+v", got)
	}
	if !got.HasRole("cms-editor") || got.HasRole("admin") {
		t.Errorf("roles did not survive: %v", got.Roles)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

// The browser must never be able to read or forge a session. Every mutation of
// a valid token has to come back as ErrInvalidToken, never as a usable identity.
func TestVerifyRejectsForgedTokens(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	s := testStore(t, func() time.Time { return now }, 0)
	valid, _, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(valid, tokenPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		t.Fatal(err)
	}

	flip := func(i int) string {
		c := make([]byte, len(raw))
		copy(c, raw)
		c[i] ^= 0x01
		return tokenPrefix + base64.RawURLEncoding.EncodeToString(c)
	}

	// A different secret must not open our blobs.
	other, err := NewStore(Options{Secret: []byte("ffffffffffffffffffffffffffffffff"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	foreign, _, err := other.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"empty", "", ErrNoToken},
		{"no prefix", body, ErrInvalidToken},
		{"wrong prefix", "pkd0_" + body, ErrInvalidToken},
		{"not base64", tokenPrefix + "!!!not base64!!!", ErrInvalidToken},
		{"truncated", tokenPrefix + body[:len(body)/2], ErrInvalidToken},
		{"empty body", tokenPrefix, ErrInvalidToken},
		{"flipped nonce bit", flip(0), ErrInvalidToken},
		{"flipped ciphertext bit", flip(len(raw) / 2), ErrInvalidToken},
		{"flipped tag bit", flip(len(raw) - 1), ErrInvalidToken},
		{"sealed under another secret", foreign, ErrInvalidToken},
		{"plain json", tokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker","roles":["cms-editor"]}`)), ErrInvalidToken},
		{"oversized", tokenPrefix + strings.Repeat("A", maxTokenLen), ErrInvalidToken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Verify(tc.token)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify(%.20q) err = %v, want %v", tc.token, err, tc.want)
			}
			if got.Subject != "" {
				t.Fatalf("a rejected token still yielded an identity: %+v", got)
			}
		})
	}
}

func TestVerifyExpiry(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	now := base
	s := testStore(t, func() time.Time { return now }, time.Hour)

	token, expires, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		at   time.Time
		want error
	}{
		{"just issued", base, nil},
		{"one second before expiry", expires.Add(-time.Second), nil},
		{"exactly at expiry", expires, ErrExpired},
		{"after expiry", expires.Add(time.Second), ErrExpired},
		{"long after expiry", expires.Add(100 * 24 * time.Hour), ErrExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now = tc.at
			_, err := s.Verify(token)
			if tc.want == nil && err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Verify err = %v, want %v", err, tc.want)
			}
		})
	}
}

// Revocation is the escape hatch for a role removed in Keycloak before the
// session would have expired on its own.
func TestRevoke(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	now := base
	s := testStore(t, func() time.Time { return now }, time.Hour)

	a, _, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two sessions for the same identity produced the same token")
	}

	if err := s.Revoke(a); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := s.Verify(a); !errors.Is(err, ErrRevoked) {
		t.Errorf("revoked token: err = %v, want ErrRevoked", err)
	}
	if _, err := s.Verify(b); err != nil {
		t.Errorf("revoking one session invalidated another: %v", err)
	}
	if err := s.Revoke(a); err != nil {
		t.Errorf("revoking twice should be a no-op, got %v", err)
	}
	if err := s.Revoke("pkd1_garbage"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Revoke(garbage) = %v, want ErrInvalidToken", err)
	}

	// An expired token reports expiry rather than revocation, and revoking it
	// is a no-op that must not grow the deny list.
	now = base.Add(2 * time.Hour)
	if err := s.Revoke(b); err != nil {
		t.Errorf("Revoke(expired) = %v, want nil", err)
	}
	if _, err := s.Verify(b); !errors.Is(err, ErrExpired) {
		t.Errorf("expired token: err = %v, want ErrExpired", err)
	}
	s.mu.Lock()
	n := len(s.revoked)
	s.mu.Unlock()
	if n != 0 {
		t.Errorf("deny list holds %d entries after everything expired, want 0", n)
	}
}

func TestHasRole(t *testing.T) {
	id := Identity{Roles: []string{"membership", "cms-editor"}}
	tests := []struct {
		role string
		want bool
	}{
		{"cms-editor", true},
		{"membership", true},
		{"", false},
		{"CMS-EDITOR", false},
		{"cms-edito", false},
		{"cms-editorr", false},
		{"admin", false},
	}
	for _, tc := range tests {
		if got := id.HasRole(tc.role); got != tc.want {
			t.Errorf("HasRole(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
	if (Identity{}).HasRole("cms-editor") {
		t.Error("an identity with no roles must not have one")
	}
}

// Editing requires every role in the set. Anything that reports "nothing
// missing" for an editor holding only some of them hands the website to
// whoever kept one role after losing the other.
func TestMissingRoles(t *testing.T) {
	required := []string{"membership", "prodeko-org-admin"}
	tests := []struct {
		name  string
		roles []string
		want  []string
	}{
		{"both roles", []string{"membership", "prodeko-org-admin"}, nil},
		{"both plus unrelated extras", []string{"admin", "prodeko-org-admin", "offline_access", "membership"}, nil},
		{"membership only", []string{"membership"}, []string{"prodeko-org-admin"}},
		{"admin only, membership lapsed", []string{"prodeko-org-admin"}, []string{"membership"}},
		{"neither", []string{"prodeko-external-member"}, required},
		{"no roles at all", nil, required},
		{"near miss", []string{"membership", "prodeko-org-admins"}, []string{"prodeko-org-admin"}},
		{"case mismatch", []string{"membership", "Prodeko-Org-Admin"}, []string{"prodeko-org-admin"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Identity{Roles: tc.roles}.MissingRoles(required)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("MissingRoles(%v) = %v, want %v", tc.roles, got, tc.want)
			}
		})
	}
}

// Tokens outlive the process that issued them, which is the whole point of
// sealing them rather than keeping a server-side table.
func TestTokensSurviveRestart(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	before := testStore(t, clock, time.Hour)
	token, _, err := before.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	after := testStore(t, clock, time.Hour)
	if _, err := after.Verify(token); err != nil {
		t.Fatalf("token did not survive a restart: %v", err)
	}
	// ...but the revocation list does not, which is a documented property.
	if err := before.Revoke(token); err != nil {
		t.Fatal(err)
	}
	if _, err := after.Verify(token); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
